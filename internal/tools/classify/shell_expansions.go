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

// underscoreUnknown is a lineVars key that can never be a real shell identifier (a bare "$" alone is
// never a variable name), used as a sentinel: once set, $_ is unknowable for the rest of the line
// (P3-R6) and note leaves it alone from here on, even for a later segment that looks like a plain
// command.
const underscoreUnknown = "$_"

// poisonUnderscore records that $_ can no longer be tracked: a condition or a loop body can run its
// commands zero, one or many times, and a subshell's own $_ never reaches the parent shell, so this
// classifier's flat, segment-by-segment model cannot know its value from here on. $_ becomes the most
// dangerous kind (optionValue) and stays that way for the rest of the line.
func (v lineVars) poisonUnderscore() {
	v[underscoreUnknown] = optionValue
	v["_"] = optionValue
}

func (v lineVars) underscorePoisoned() bool {
	_, poisoned := v[underscoreUnknown]
	return poisoned
}

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
// are recorded either way. $_, the previous command's last argument, is tracked the same way, but
// only across a line of plain commands joined by ; && || and | (P3-R6): a control word (opening or
// closing), a case head, a brace group or a parenthesis anywhere on the line poisons it permanently
// (poisonUnderscore), since none of them changes bash's real $_ the way a flat, segment-by-segment
// model would assume.
func (v lineVars) note(words []string, parenthesized bool) {
	if parenthesized {
		v.poisonUnderscore()
	}
	i := 0
	for i < len(words) && openingWords[words[i]] {
		i++
	}
	stripped := words[i:]
	switch {
	case len(stripped) == 0, len(stripped) == 1 && closingWords[stripped[0]]:
		v.poisonUnderscore()
		return
	case stripped[0] == "case":
		v.poisonUnderscore()
		return
	case stripped[0] == "for":
		kind := optionValue // no "in", or an empty list: an unknown number of iterations, or none
		if len(stripped) > 3 && stripped[2] == "in" {
			kind = literalValue
			for _, w := range stripped[3:] {
				kind = min(kind, valueKind(w))
			}
		}
		v.raise(stripped[1], kind)
		v.poisonUnderscore()
		return
	}
	if i > 0 { // an opening word (if, while, !, do, ...) ran what remains: how often is unknown
		v.poisonUnderscore()
	}
	for _, w := range stripped[:assignmentsEnd(stripped)] {
		name, value, _ := strings.Cut(w, "=")
		v.raise(name, valueKind(value))
	}
	if v.underscorePoisoned() {
		return
	}
	if command := stripped[assignmentsEnd(stripped):]; len(command) > 0 {
		v["_"] = valueKind(command[len(command)-1])
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
	if rest != "" && strings.IndexByte("0123456789@*#?$!-_", rest[0]) >= 0 {
		return expansionRef{Name: rest[:1]}, 1
	}
	return expansionRef{}, 0
}

// injects reports a word that expands a value the line controls and may split into options: a
// variable the line set to a value with an option or an expansion, a substitution operator whose
// literal word is an option, ${...} with an assigning operator, or a command substitution. $_ is the
// last argument of the previous command, which note records like any variable; a variable the line
// does not set is taracode's environment, which the user controls. Any reference immediately followed
// by "-" in the same word injects too (P3-R7), whatever it reads: the substitution operators can
// expand to nothing (${X:+w} and ${X+w} when X is unset, ${X:-} and ${X-} when X is unset or empty),
// and a plain $X can itself be an empty variable, so the "-" would then start the word instead of
// following a value. It rescans word itself (rather than calling references) so it can see the byte
// after each reference ends.
//
// harmless is true when the program that receives the word only prints its arguments (echo, :, true,
// false, or a for-list, which runs nothing): ruling R3. It relaxes the two shapes that are dangerous
// only because an extra option changes what the program does - a command-substitution marker (a bare
// "$" the parser left when it split the word at the substitution's "(") and a variable set to an
// option value. The other shapes stay a mutation whatever the program: a reference followed by "-", an
// assigning or otherwise opaque ${...} form (it can change shell state or hide any output), and a
// substitution operator whose literal word is an option.
func (v lineVars) injects(word string, harmless bool) bool {
	for i := 0; i < len(word); i++ {
		if word[i] != '$' {
			continue
		}
		r, n := reference(word[i+1:])
		end := i + 1 + n
		if end < len(word) && word[end] == '-' {
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

// expansionResult is the mutation for a command whose word expands a value the line controls.
func expansionResult(program, word string) Result {
	return mutate(program, "the word "+word+" expands a value this line sets or can set, which unquoted can add "+
		"options to "+program)
}

// expansionCheck finds the first word of a segment (its words, assignments included, and its
// redirect targets) that expands a value the line controls, and the first argument whose brace
// expansion yields an option. ok is false when there is none. The program is option-harmless when it
// only prints its arguments; a for-header has no args, so its list may hold a substitution (R3).
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
		if braceOption(w) {
			return mutate(program, "the brace expansion "+w+" gives "+program+" an option"), true
		}
	}
	return Result{}, false
}

// braceLimit bounds the words a brace expansion is followed to; past it, it counts as an option.
const braceLimit = 64

// braceOption reports a word whose brace expansion ({a,b} or {1..3}, which bash performs before the
// command runs) yields a word that starts with "-": find . {-delete,-print} deletes. The literal
// word never shows it.
func braceOption(word string) bool {
	if _, _, _, ok := braceSplit(word); !ok {
		return false
	}
	queue := []string{word}
	for n := 0; len(queue) > 0; n++ {
		if n > braceLimit {
			return true
		}
		w := queue[0]
		queue = queue[1:]
		prefix, alternatives, suffix, ok := braceSplit(w)
		if !ok {
			if strings.HasPrefix(w, "-") {
				return true
			}
			continue
		}
		for _, a := range alternatives {
			queue = append(queue, prefix+a+suffix)
		}
	}
	return false
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
