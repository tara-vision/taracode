package classify

import (
	"path"
	"strings"

	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// innerDepth is how many levels of strings run as lines (sh -c "sh -c '...'") denyLines reads.
const innerDepth = 2

// shellNames run the string after their -c as a command line.
var shellNames = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "ash": true}

// denyLines are the forms of a line the deny patterns read besides the whole of it: each simple command,
// the same without the one-word options before its first operand (terraform -chdir=. destroy reads as
// terraform destroy), and the strings a shell's -c, eval or a $(...) runs, read as lines of their own.
func denyLines(parsed shellwords.Result, depth int) []string {
	var out []string
	for _, seg := range parsed.Segments {
		simple, _ := simpleCommand(seg.Words)
		words := simple[assignmentsEnd(simple):]
		if len(words) == 0 {
			continue
		}
		out = append(out, strings.Join(words, " "))
		if trimmed := withoutLeadingOptions(words); len(trimmed) != len(words) {
			out = append(out, strings.Join(trimmed, " "))
		}
		if depth > 0 {
			for _, inner := range innerLines(words) {
				out = append(out, linesOf(inner, depth-1)...)
			}
		}
	}
	if depth > 0 {
		for _, body := range parsed.Substitutions {
			out = append(out, linesOf(body, depth-1)...)
		}
	}
	return out
}

// linesOf parses a string run as a command line and returns its deny lines; a string that does not parse
// has none.
func linesOf(line string, depth int) []string {
	parsed, err := shellwords.Split(line)
	if err != nil {
		return nil
	}
	return denyLines(parsed, depth)
}

// withoutLeadingOptions drops the one-word options between a program and its first operand. An option
// whose value is a word of its own stays: its value cannot be told from an operand.
func withoutLeadingOptions(words []string) []string {
	i := 1
	for i < len(words) && strings.HasPrefix(words[i], "-") && words[i] != "-" && words[i] != "--" {
		i++
	}
	return append([]string{words[0]}, words[i:]...)
}

// innerLines are the strings a command hands to a shell's -c (a short option holding c, as -ec) or to
// eval, to run as a line of their own.
func innerLines(words []string) []string {
	w := unwrap(words)
	if len(w.words) == 0 {
		return nil
	}
	prog, rest := path.Base(w.words[0]), w.words[1:]
	switch {
	case prog == "eval" && len(rest) > 0:
		return []string{strings.Join(rest, " ")}
	case shellNames[prog]:
		for i, word := range rest {
			if strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.ContainsRune(word, 'c') &&
				i+1 < len(rest) {
				return []string{rest[i+1]}
			}
		}
	}
	return nil
}
