package classify

import (
	"math"
	"regexp"
	"strconv"
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
// ({a,$X}-delete) is caught by braceInjects instead, leaf by leaf. A ${...} reference in a word that
// also holds an unmatched "}" is opaque and injects (strippedBrace, ruling P3-R32): the stray "}" is
// the trace of a quoted or escaped brace shellwords removed, so reference() closed the ${ at the wrong
// "}".
//
// harmless is true when the program that receives the word only prints its arguments (echo, :, true,
// false, or a for-list, which runs nothing): ruling R3. It relaxes the two shapes that are dangerous
// only because an extra option changes what the program does - a command-substitution marker (a bare
// "$" the parser left when it split the word at the substitution's "(") and a variable set to an
// option value. The other shapes stay a mutation whatever the program: $_, the leading-run dash rule,
// a stripped brace, a substitution operator whose word holds a brace (P3-R40: a stripped quote may
// have hidden the real end of the ${...}), an assigning or otherwise opaque ${...} form (it can change
// shell state or hide any output), and a substitution operator whose literal word is an option.
func (v lineVars) injects(word string, harmless bool) bool {
	if leadingRunEndsInDash(word) || strippedBrace(word) {
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
		if r.Substitutes {
			// P3-R40: a brace ({ or }) in the operator word may be the trace of a quote or escape
			// shellwords removed, so reference() can have closed the ${...} at the wrong "}" and the
			// classifier's word can differ from the shell's; the form is opaque and injects for every
			// program (find . ${X:+{}}-delete, find . ${X:-a{b}}-delete, echo ${X:+{}}).
			if strings.ContainsAny(r.Word, "{}") {
				return true
			}
			if valueKind(strings.Trim(r.Word, `"'`)) == optionValue {
				return true
			}
		}
		i += n
	}
	return false
}

// leadingRunEndsInDash reports whether word, with its whole leading run of $ references removed one
// after another from byte 0, is left with the run followed immediately by "-" (P3-R20, amending
// P3-R14a). A run can be more than one reference ($@$@-delete, $X$Y-delete): every reference in the
// run can expand to nothing, whatever it reads, so the whole run can vanish and leave the "-" to start
// the word. reference() consumes each one - a name, a ${...} form, or a special or positional
// parameter; a "$" it does not recognize (a bare "$", or the marker shellwords leaves where it split a
// $(...) off into its own word) returns n == 0, and the walk still steps one byte past it so a run of
// them does not stall. It walks the word with reference() itself, rather than calling references, so
// it can see the byte right after the run. Literal text before the run, or anywhere the run stops
// (word[pos] is not "$"), ends the check there: only a run starting at byte 0 can expose a leading "-".
func leadingRunEndsInDash(word string) bool {
	pos := 0
	for pos < len(word) && word[pos] == '$' {
		_, n := reference(word[pos+1:])
		pos += 1 + n
	}
	return pos > 0 && pos < len(word) && word[pos] == '-'
}

// strippedBrace reports a word that holds both a ${...} reference and an unmatched "}" - one that
// closes no "{" or "${" opened earlier in the same word (ruling P3-R32). shellwords removes quotes and
// escapes, so find . ${X:+"}"}-delete, find . ${X:+\}}-delete and find . ${X:+'}'}-delete all reach
// the classifier as ${X:+}}-delete: reference() then closes the ${ at the first "}" and reads the rest
// as a literal, while every shell closes it at the second and the word expands to an option (-delete).
// The stray "}" is the trace of the brace a quote hid, so the ${...} form is opaque and injects for
// every program, harmless or not, the same as any other opaque ${...} form.
func strippedBrace(word string) bool {
	if !strings.Contains(word, "${") {
		return false
	}
	depth := 0
	for i := 0; i < len(word); i++ {
		switch word[i] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return true
			}
			depth--
		}
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
		leaf, whole, ok := v.braceInjects(w, harmless)
		switch {
		case ok && whole: // more members than braceLimit, or a sequence the classifier cannot enumerate
			return mutate(program, "the brace expansion "+w+" gives "+program+" an option"), true
		case ok:
			return mutate(program, "the brace expansion "+w+" can become "+leaf+", which unquoted can add "+
				"options to "+program), true
		}
	}
	return Result{}, false
}

// braceLimit bounds the words braceLeaves dequeues while following a brace expansion, and the members
// a sequence may have; past either bound, the word counts as an option.
const braceLimit = 64

// braceOption reports a word whose brace expansion ({a,b} or {1..3}, which bash performs before the
// command runs) yields a leaf that is dangerous on its own (leafDanger): it starts with "-" (find .
// {-delete,-print} deletes), or its own leading run of $ references ends right before "-" (find .
// {a,$X}-delete: bash brace-expands textually, before $X is read, so the alternative becomes
// $X-delete, and $X can expand to nothing and leave -delete) (P3-R14, P3-R20). The literal word never
// shows either danger. It does not know about the line's variables, so valueKind (which classifies a
// literal value in isolation) uses it; expansionCheck uses braceInjects instead. It keeps a sequence's
// two ends (endsOnly), since only the low bound of {x..y} can start with "-" (P3-R31 leaves this test
// as it is).
func braceOption(word string) bool {
	_, _, found := braceLeaves(word, endsOnly, leafDanger)
	return found
}

// braceInjects is braceOption plus ruling P3-R21: a brace alternative can rebuild the name of a
// variable the line set (bash brace-expands before parameter expansion, so $a{b,x} becomes $ab and
// $ax as words, not $a followed by b or x), so each leaf is also checked the way injects checks a
// plain word, with the same harmless flag expansionCheck computed for the word itself. It enumerates
// every member of a {x..y} sequence (braceSequence, ruling P3-R31), since $a{1..9} rebuilds $a5 too,
// not only its ends. leaf is the first dangerous leaf found, named in the mutation reason so it reads
// more like "$ab" than "-". whole is true when the word is dangerous as a unit rather than through one
// leaf: more members than braceLimit, or a sequence the classifier cannot enumerate (fail closed).
func (v lineVars) braceInjects(word string, harmless bool) (leaf string, whole, found bool) {
	return braceLeaves(word, braceSequence, func(w string) bool {
		return leafDanger(w) || v.injects(w, harmless)
	})
}

// braceLeaves follows every alternative of word's brace expansion, dequeuing at most braceLimit words
// (past that, word itself counts as one dangerous unit, however deep or wide the expansion), and
// returns the first fully expanded leaf dangerous reports true for. seq expands a sequence's body
// ({x..y}); when it cannot (an opaque sequence) the word is one dangerous unit too. found is false
// when word has no brace expansion, or none of its leaves are dangerous. whole is true when the word
// is dangerous as a unit (over the limit or an opaque sequence), so the caller reports it as a single
// option rather than naming a leaf.
func braceLeaves(word string, seq sequencer, dangerous func(string) bool) (leaf string, whole, found bool) {
	if _, _, _, _, ok := braceSplit(word, seq); !ok {
		return "", false, false
	}
	queue := []string{word}
	for n := 0; len(queue) > 0; n++ {
		if n > braceLimit {
			return word, true, true
		}
		w := queue[0]
		queue = queue[1:]
		prefix, alternatives, suffix, opaque, ok := braceSplit(w, seq)
		switch {
		case opaque:
			return w, true, true
		case !ok:
			if dangerous(w) {
				return w, false, true
			}
			continue
		}
		for _, a := range alternatives {
			queue = append(queue, prefix+a+suffix)
		}
	}
	return "", false, false
}

// leafDanger reports a fully brace-expanded leaf that starts with "-", or whose own leading run of $
// references ends right before "-" (leadingRunEndsInDash): the same rule injects applies to a whole
// word, applied to the leaf instead (P3-R14, P3-R20).
func leafDanger(w string) bool {
	return strings.HasPrefix(w, "-") || leadingRunEndsInDash(w)
}

// sequencer turns a sequence-shaped brace body ("x..y" or "x..y..step", see sequenceShaped) into the
// words it expands to. opaque is true when the sequence cannot be followed (an integer and a letter,
// letters of two cases, a zero step, an operand or step past 32 bits, or more members than
// braceLimit): the caller then treats the whole word as one option and fails closed.
type sequencer func(body string) (members []string, opaque bool)

// sequenceShaped reports a brace body shaped like a sequence: two non-empty operands, each a signed
// integer or a single letter, and an optional integer step (ruling P3-R42). Any other body with ".."
// in it ({..image}, {main..dev}, {..}) is no sequence in bash 3.2 or bash 5.3 (dash has no brace
// expansion at all), so braceSplit leaves it literal. The mixed shapes it admits ({1..a}, {a..Z}) and a
// zero step ({1..9..0}) still reach the sequencer, which calls them opaque.
func sequenceShaped(body string) bool {
	parts := strings.Split(body, "..")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	for _, p := range parts[:2] {
		if !isInteger(p) && !isLetter(p) {
			return false
		}
	}
	return len(parts) == 2 || isInteger(parts[2])
}

// isInteger reports a signed decimal integer as a sequence operand is written: an optional "+" or "-",
// then one or more digits.
func isInteger(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// sequenceInt parses a sequence operand or step. ok is false when it does not parse as an int64 or its
// absolute value exceeds 2147483647 (ruling P3-R42): sh on macOS is bash 3.2, which truncates an
// operand to 32 bits ({4294967297..4294967297} rebuilds $a1 there), bash 5 reads it whole, and past
// int64 the arithmetic would wrap, so beyond that bound the shells and the classifier disagree.
func sequenceInt(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v > math.MaxInt32 || v < -math.MaxInt32 {
		return 0, false
	}
	return v, true
}

// endsOnly keeps a sequence's two ends, enough for the leading-dash test braceOption applies: only the
// low bound, or a negative one, can start with "-". It never reports opaque, matching the pre-P3-R31
// behavior braceOption keeps.
func endsOnly(body string) ([]string, bool) {
	lo, hi, _ := strings.Cut(body, "..")
	return []string{lo, hi}, false
}

// braceSequence enumerates the words a sequence-shaped brace body expands to, the way bash does, so
// braceInjects sees the name each leaf rebuilds ($a{1..9} rebuilds $a1..$a9, not just $a1 and $a9). It
// follows a numeric range (with an optional step, and both the plain and, when the operands are
// zero-padded, the padded form of each member, since sh and bash pad differently) and a
// single-character letter range within one case. opaque is true (fail closed) for a shape it cannot
// follow - an integer and a letter ({1..a}), letters of two cases ({a..Z}), a zero step ({1..9..0}),
// an operand or step sequenceInt rejects (past 32 bits, P3-R42) - or one with more than braceLimit
// members (P3-R31).
func braceSequence(body string) ([]string, bool) {
	parts := strings.Split(body, "..")
	if len(parts) < 2 || len(parts) > 3 {
		return nil, true
	}
	step := int64(1)
	if len(parts) == 3 {
		s, ok := sequenceInt(parts[2])
		if !ok || s == 0 {
			return nil, true
		}
		if step = s; step < 0 {
			step = -step
		}
	}
	lo, hi := parts[0], parts[1]
	if isInteger(lo) && isInteger(hi) {
		a, oka := sequenceInt(lo)
		b, okb := sequenceInt(hi)
		if !oka || !okb {
			return nil, true
		}
		return numericMembers(a, b, step, padWidth(lo, hi))
	}
	if isLetter(lo) && isLetter(hi) && sameLetterCase(lo[0], hi[0]) {
		return letterMembers(lo[0], hi[0], step), false
	}
	return nil, true
}

// numericMembers lists the integers from a to b inclusive at step, each as its plain decimal form and,
// when width > 0 (the operands are zero-padded), its zero-padded form too, so both the sh and the bash
// name a leaf could rebuild are checked. a, b and step come from sequenceInt, so the arithmetic stays
// far inside int64. opaque is true, and members nil, when the count exceeds braceLimit; otherwise
// members holds every member, at least a.
func numericMembers(a, b, step int64, width int) (members []string, opaque bool) {
	span, dir := b-a, int64(1)
	if span < 0 {
		span, dir = -span, -1
	}
	count := span/step + 1
	if count > braceLimit {
		return nil, true
	}
	for v, n := a, int64(0); n < count; v, n = v+dir*step, n+1 {
		members = append(members, strconv.FormatInt(v, 10))
		if width > 0 {
			members = append(members, padNumber(v, width))
		}
	}
	return members, false
}

const (
	lowerLetters = "abcdefghijklmnopqrstuvwxyz"
	upperLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// letterMembers lists the characters from lo to hi inclusive at step, walking the one-case alphabet
// (lo and hi are same-case letters, the caller checked) and keeping every step-th one, so no arithmetic
// narrows to a byte. A letter range has at most 26 members, under braceLimit, so it is never opaque;
// the result always holds at least lo.
func letterMembers(lo, hi byte, step int64) []string {
	alpha := lowerLetters
	if lo >= 'A' && lo <= 'Z' {
		alpha = upperLetters
	}
	loi, hii := strings.IndexByte(alpha, lo), strings.IndexByte(alpha, hi)
	dir := 1
	if hii < loi {
		dir = -1
	}
	var members []string
	for j := loi; ; j += dir {
		if int64(abs(j-loi))%step == 0 {
			members = append(members, alpha[j:j+1])
		}
		if j == hii {
			return members
		}
	}
}

// padWidth is the zero-pad width a numeric range uses: the longer operand's length when either operand
// is written with a leading zero, else 0 (no padding).
func padWidth(lo, hi string) int {
	if !zeroPadded(lo) && !zeroPadded(hi) {
		return 0
	}
	return max(len(lo), len(hi))
}

// zeroPadded reports a number written with a leading zero (01, -007), which asks bash to pad the range.
func zeroPadded(s string) bool {
	s = strings.TrimLeft(s, "+-")
	return len(s) > 1 && s[0] == '0'
}

// padNumber writes v zero-padded to width, keeping a leading "-" outside the padding.
func padNumber(v int64, width int) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	digits := strconv.FormatInt(v, 10)
	for len(sign)+len(digits) < width {
		digits = "0" + digits
	}
	return sign + digits
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func isLetter(s string) bool {
	return len(s) == 1 && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z')
}

func sameLetterCase(a, b byte) bool {
	return (a >= 'a' && a <= 'z') == (b >= 'a' && b <= 'z')
}

// braceSplit finds the first brace expansion of a word: a { (not ${) whose matching } encloses a
// comma at its own level or a sequence-shaped body (sequenceShaped); any other body, one holding ".."
// included, is literal and the scan moves on (P3-R42). It returns what comes before and after it and
// its alternatives. seq expands a sequence's body; opaque is true when seq cannot follow the sequence,
// so the caller treats the whole word as one dangerous unit and fails closed.
func braceSplit(w string, seq sequencer) (prefix string, alternatives []string, suffix string, opaque, ok bool) {
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
		case sequenceShaped(w[i+1 : end]):
			members, seqOpaque := seq(w[i+1 : end])
			if seqOpaque {
				return w[:i], nil, w[end+1:], true, true
			}
			alternatives = members
		default:
			continue
		}
		return w[:i], alternatives, w[end+1:], false, true
	}
	return "", nil, "", false, false
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
