package classify

import (
	"regexp"
	"strings"
)

// lineVars are the variables a shell line sets for its later commands, a for loop's variable and
// a NAME=value segment, each with the kind of its value. Unquoted, a variable splits into words,
// and a word that starts with "-" is an option to the command it lands in (for x in -delete; do
// find . $x; done deletes), so only a plain value keeps a read a read. A variable the line does not
// set is taracode's environment, which the user controls.
type lineVars map[string]varKind

// varKind is what a variable's value can expand to.
type varKind int

const (
	optionValue  varKind = iota // a word of it starts with "-", or it holds an expansion
	globValue                   // plain words with a glob, expanded against file names at run time
	literalValue                // plain words, known now
)

var identifierPrefix = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

// raise records a variable's kind, keeping the more dangerous of what was already recorded and kind:
// optionValue is the most dangerous, then globValue, then literalValue (min, in their declared order),
// the same rule the for-list scan below already applies across a list's own words. A kind never
// downgrades: a command-prefix assignment (x=1 true) does not persist past its command, and an empty
// or in-less for loop may not run its body at all, so the earlier, more dangerous value can still be
// the real one.
func (v lineVars) raise(name string, kind varKind) {
	if old, set := v[name]; set {
		kind = min(kind, old)
	}
	v[name] = kind
}

// note records the variables a segment sets for the segments after it: a for loop's variable, and
// the NAME=value words in front of a command or alone. In front of a command they apply to that
// command only, except before a POSIX special builtin (exec, :, set, ...), where they stay set; they
// are recorded either way. $_ is not tracked (P3-R13): bash's real $_ depends on control flow,
// pipelines and even the shell (bash 3 vs 5, dash) in ways this flat, segment-by-segment model
// cannot follow, so injects treats every $_ reference as unknown instead of consulting this map.
func (v lineVars) note(words []string) {
	i := 0
	for i < len(words) && openingWords[words[i]] {
		i++
	}
	words = words[i:]
	if len(words) >= 2 && words[0] == "for" {
		kind := optionValue // no "in", or an empty list: an unknown number of iterations, or none
		if len(words) > 3 && words[2] == "in" {
			kind = literalValue
			for _, w := range words[3:] {
				kind = min(kind, valueKind(w))
			}
		}
		v.raise(words[1], kind)
		return
	}
	for _, w := range words[:assignmentsEnd(words)] {
		name, value, _ := strings.Cut(w, "=")
		v.raise(name, valueKind(value))
	}
}

// valueKind classifies a value: an option or an expansion in it (a for list is brace-expanded too),
// a glob, or plain words.
func valueKind(value string) varKind {
	if strings.ContainsAny(value, "$`") || braceOption(value) {
		return optionValue
	}
	for _, f := range strings.Fields(value) {
		if strings.HasPrefix(f, "-") {
			return optionValue
		}
	}
	if strings.ContainsAny(value, "*?[") {
		return globValue
	}
	return literalValue
}

// expansionRef is one $ expansion of a word: the variable it reads (Name; "" for a substitution, a
// lone $ or a ${...} form whose value the line cannot know), and, for the substitution operators
// ${name:-word}, ${name-word}, ${name:+word} and ${name+word}, the literal word the shell may use
// instead (Word, Substitutes true). ${name:=word} assigns and ${name?word} exits, so they stay unknown.
type expansionRef struct {
	Name        string
	Word        string
	Substitutes bool
}

func references(word string) []expansionRef {
	var out []expansionRef
	for i := 0; i < len(word); i++ {
		if word[i] == '$' {
			ref, n := reference(word[i+1:])
			out = append(out, ref)
			i += n
		}
	}
	return out
}

// reference reads the expansion after a $ and returns it with the number of bytes it takes.
func reference(rest string) (expansionRef, int) {
	if name := identifierPrefix.FindString(rest); name != "" {
		return expansionRef{Name: name}, len(name)
	}
	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return expansionRef{}, len(rest)
		}
		inner := rest[1:end]
		name := identifierPrefix.FindString(inner)
		if name != "" && name == inner {
			return expansionRef{Name: name}, end + 1
		}
		if name != "" {
			op := inner[len(name):]
			for _, prefix := range []string{":-", ":+", "-", "+"} {
				if strings.HasPrefix(op, prefix) {
					return expansionRef{Name: name, Word: op[len(prefix):], Substitutes: true}, end + 1
				}
			}
		}
		return expansionRef{}, end + 1
	}
	if rest != "" && strings.IndexByte("0123456789@*#?$!-", rest[0]) >= 0 {
		return expansionRef{Name: rest[:1]}, 1
	}
	return expansionRef{}, 0
}

// injects reports a word that expands a value the line controls and may split into options: a
// variable the line set to a value with an option or an expansion, a substitution operator whose
// literal word is an option, ${...} with an assigning operator, a command substitution, or $_ (P3-R13:
// never tracked, since bash's real $_ depends on control flow, pipelines and even the shell in ways
// this classifier cannot follow, so every $_ reference is unknown). A variable the line does not set
// is taracode's environment, which the user controls. The word's leading run of references (P3-R20;
// see leadingRunEndsInDash) landing on "-" injects too: every reference in the run can expand to
// nothing, whatever it reads, so the "-" would then start the word instead of following a value.
// Literal text in front of the run (app=$APP-api) means the word can never start with "-" however the
// run resolves, so the rule only ever looks at byte 0; the same danger for a brace alternative
// ({a,$X}-delete) is caught in braceOption instead, leaf by leaf. It rescans word itself (rather than
// calling references) so it can see the byte after each reference ends.
//
// harmless is true when the program that receives the word only prints its arguments (echo, :, true,
// false, or a for-list, which runs nothing): ruling R3. It relaxes the two shapes that are dangerous
// only because an extra option changes what the program does - a command-substitution marker (a bare
// "$" the parser left when it split the word at the substitution's "(") and a variable set to an
// option value. The other shapes stay a mutation whatever the program: $_, the leading-run dash rule,
// an assigning or otherwise opaque ${...} form (it can change shell state or hide any output), and a
// substitution operator whose literal word is an option.
func (v lineVars) injects(word string, harmless bool) bool {
	if leadingRunEndsInDash(word) {
		return true
	}
	for i := 0; i < len(word); i++ {
		if word[i] != '$' {
			continue
		}
		r, n := reference(word[i+1:])
		if r.Name == "_" {
			return true
		}
		if r.Name == "" {
			if n == 0 {
				return !harmless // a command-substitution marker or a bare "$"
			}
			return true // an assigning or opaque ${...} form
		}
		if kind, set := v[r.Name]; set && kind == optionValue {
			return !harmless
		}
		if r.Substitutes && valueKind(strings.Trim(r.Word, `"'`)) == optionValue {
			return true
		}
		i += n
	}
	return false
}

// leadingRunEndsInDash reports whether word, with its whole leading run of $ references removed one
// after another from byte 0, is left with the run followed immediately by "-" (P3-R20, amending
// P3-R14a). A run can be more than one reference ($@$@-delete, $X$Y-delete): bash brace-expands and
// command-substitutes before parameter expansion, but every reference in the run, whatever it reads -
// a name, a ${...} form, a special parameter or a bare "$" substitution marker, each consumed with
// reference() - can still expand to nothing, so the whole run can vanish and leave the "-" to start
// the word. Literal text before the run, or anywhere the run stops (word[pos] is not "$"), ends the
// check there: only a run starting at byte 0 can expose a leading "-".
func leadingRunEndsInDash(word string) bool {
	pos := 0
	for pos < len(word) && word[pos] == '$' {
		_, n := reference(word[pos+1:])
		pos += 1 + n
	}
	return pos > 0 && pos < len(word) && word[pos] == '-'
}

// expansionResult is the mutation for a command whose word expands a value the line controls.
func expansionResult(program, word string) Result {
	return mutate(program, "the word "+word+" expands a value this line sets or can set, which unquoted can add "+
		"options to "+program)
}

// expansionCheck finds the first word of a segment (its words, assignments included, and its
// redirect targets) that expands a value the line controls, and the first argument whose brace
// expansion yields a dangerous leaf. ok is false when there is none. The program is option-harmless
// when it only prints its arguments; a for-header has no args, so its list may hold a substitution
// (R3). A brace leaf is checked the same way a plain word is (braceInjects, ruling P3-R21), since
// bash brace-expands before parameter expansion: {a,$X} can rebuild the name of a variable the line
// set (sort $a{b,x} reads $ab, not $a), not only the literal shape of a leaf (leafDanger).
func (v lineVars) expansionCheck(words, redirects, args []string) (Result, bool) {
	program := first(args)
	harmless := args == nil || optionHarmless[program]
	targets := make([]string, 0, len(redirects))
	for _, r := range redirects {
		_, target, _ := strings.Cut(r, " ")
		targets = append(targets, target)
	}
	for _, w := range append(append([]string{}, words...), targets...) {
		if v.injects(w, harmless) {
			return expansionResult(program, w), true
		}
	}
	for _, w := range tail(args) {
		if leaf, ok := v.braceInjects(w, harmless); ok {
			return mutate(program, "the brace expansion "+w+" can become "+leaf+", which unquoted can add "+
				"options to "+program), true
		}
	}
	return Result{}, false
}

// braceLimit bounds the words a brace expansion is followed to; past it, it counts as an option.
const braceLimit = 64

// braceOption reports a word whose brace expansion ({a,b} or {1..3}, which bash performs before the
// command runs) yields a leaf that is dangerous on its own (leafDanger): it starts with "-" (find .
// {-delete,-print} deletes), or its own leading run of $ references ends right before "-" (find .
// {a,$X}-delete: bash brace-expands textually, before $X is read, so the alternative becomes
// $X-delete, and $X can expand to nothing and leave -delete) (P3-R14, P3-R20). The literal word never
// shows either danger. It does not know about the line's variables, so valueKind (which classifies a
// literal value in isolation) uses it; expansionCheck uses braceInjects instead.
func braceOption(word string) bool {
	_, found := braceLeaves(word, leafDanger)
	return found
}

// braceInjects is braceOption plus ruling P3-R21: a brace alternative can rebuild the name of a
// variable the line set (bash brace-expands before parameter expansion, so $a{b,x} becomes $ab and
// $ax as words, not $a followed by b or x), so each leaf is also checked the way injects checks a
// plain word, with the same harmless flag expansionCheck computed for the word itself. leaf is the
// first dangerous leaf found, named in the mutation reason so it reads more like "$ab" than "-".
func (v lineVars) braceInjects(word string, harmless bool) (leaf string, found bool) {
	return braceLeaves(word, func(w string) bool {
		return leafDanger(w) || v.injects(w, harmless)
	})
}

// braceLeaves follows every alternative of word's brace expansion, nested up to braceLimit levels
// (past it, word itself counts as its own dangerous leaf), and returns the first fully expanded leaf
// dangerous reports true for. found is false when word has no brace expansion, or none of its leaves
// are dangerous.
func braceLeaves(word string, dangerous func(string) bool) (leaf string, found bool) {
	if _, _, _, ok := braceSplit(word); !ok {
		return "", false
	}
	queue := []string{word}
	for n := 0; len(queue) > 0; n++ {
		if n > braceLimit {
			return word, true
		}
		w := queue[0]
		queue = queue[1:]
		prefix, alternatives, suffix, ok := braceSplit(w)
		if !ok {
			if dangerous(w) {
				return w, true
			}
			continue
		}
		for _, a := range alternatives {
			queue = append(queue, prefix+a+suffix)
		}
	}
	return "", false
}

// leafDanger reports a fully brace-expanded leaf that starts with "-", or whose own leading run of $
// references ends right before "-" (leadingRunEndsInDash): the same rule injects applies to a whole
// word, applied to the leaf instead (P3-R14, P3-R20).
func leafDanger(w string) bool {
	return strings.HasPrefix(w, "-") || leadingRunEndsInDash(w)
}

// braceSplit finds the first brace expansion of a word: a { (not ${) whose matching } encloses a
// comma at its own level or a sequence x..y. It returns what comes before and after it and its
// alternatives; of a sequence only its two ends, since only they can start with "-".
func braceSplit(w string) (prefix string, alternatives []string, suffix string, ok bool) {
	for i := 0; i < len(w); i++ {
		if w[i] != '{' || i > 0 && w[i-1] == '$' {
			continue
		}
		end, commas := braceEnd(w, i)
		switch {
		case end < 0:
			continue
		case len(commas) > 0:
			start := i + 1
			for _, c := range commas {
				alternatives = append(alternatives, w[start:c])
				start = c + 1
			}
			alternatives = append(alternatives, w[start:end])
		case strings.Contains(w[i+1:end], ".."):
			lo, hi, _ := strings.Cut(w[i+1:end], "..")
			alternatives = []string{lo, hi}
		default:
			continue
		}
		return w[:i], alternatives, w[end+1:], true
	}
	return "", nil, "", false
}

// braceEnd returns the index of the } that closes the { at start, and the commas at its level; -1
// when it is not closed.
func braceEnd(w string, start int) (int, []int) {
	depth := 0
	var commas []int
	for i := start; i < len(w); i++ {
		switch w[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, commas
			}
		case ',':
			if depth == 1 {
				commas = append(commas, i)
			}
		}
	}
	return -1, nil
}
