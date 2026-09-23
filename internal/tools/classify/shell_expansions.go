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

// note records the variables a segment sets for the segments after it: a for loop's variable, and
// the NAME=value words in front of a command or alone. In front of a command they apply to that
// command only, except before a POSIX special builtin (exec, :, set, ...), where they stay set; they
// are recorded either way.
func (v lineVars) note(words []string) {
	i := 0
	for i < len(words) && openingWords[words[i]] {
		i++
	}
	words = words[i:]
	if len(words) >= 2 && words[0] == "for" {
		kind := literalValue
		if len(words) > 2 && words[2] == "in" {
			for _, w := range words[3:] {
				kind = min(kind, valueKind(w))
			}
		}
		v[words[1]] = kind
		return
	}
	for _, w := range words[:assignmentsEnd(words)] {
		name, value, _ := strings.Cut(w, "=")
		v[name] = valueKind(value)
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

// references returns what each $ of a word expands: a variable name, a special parameter (one of
// 0-9 @ * # ? $ ! -), "_" for bash's last argument of the previous command, or "" for ${...} with
// an operator (${x:=y} assigns, ${x:-y} substitutes y), a substitution or a lone $.
func references(word string) []string {
	var out []string
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
func reference(rest string) (string, int) {
	if name := identifierPrefix.FindString(rest); name != "" {
		return name, len(name)
	}
	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", len(rest)
		}
		if inner := rest[1:end]; inner != "" && identifierPrefix.FindString(inner) == inner {
			return inner, end + 1
		}
		return "", end + 1
	}
	if rest != "" && strings.IndexByte("0123456789@*#?$!-", rest[0]) >= 0 {
		return rest[:1], 1
	}
	return "", 0
}

// injects reports a word that expands a value the line controls and may split into options: a
// variable the line set to a value with an option or an expansion, ${...} with an operator, $_ or a
// substitution. A glob is expanded as the same glob written as an argument would be.
func (v lineVars) injects(word string) bool {
	for _, r := range references(word) {
		if kind, set := v[r]; r == "" || r == "_" || set && kind == optionValue {
			return true
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
// expansion yields an option. ok is false when there is none.
func (v lineVars) expansionCheck(words, redirects, args []string) (Result, bool) {
	program := first(args)
	targets := make([]string, 0, len(redirects))
	for _, r := range redirects {
		_, target, _ := strings.Cut(r, " ")
		targets = append(targets, target)
	}
	for _, w := range append(append([]string{}, words...), targets...) {
		if v.injects(w) {
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
