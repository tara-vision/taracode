package classify

import (
	"path"
	"slices"
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
			for _, inner := range innerLines(words, segmentInput(seg)) {
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
// eval, to run as a line of their own, and the script a shell without -c reads from its input.
func innerLines(words, input []string) []string {
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
		if scriptOnInput(rest) {
			return input
		}
	}
	return nil
}

// segmentInput is what a segment's here-documents and here-strings hand its program on its standard input.
func segmentInput(seg shellwords.Segment) []string {
	input := slices.Clone(seg.Heredocs)
	for _, r := range seg.Redirects {
		if s, ok := strings.CutPrefix(r, "<<< "); ok {
			input = append(input, s)
		}
	}
	return input
}

// scriptOnInput reports whether a shell run without -c reads its script from its standard input: it names no
// script file, or a -s says so. -o and +o take an option's name.
func scriptOnInput(rest []string) bool {
	for i := 0; i < len(rest); i++ {
		switch w := rest[i]; {
		case w == "-o" || w == "+o":
			i++
		case strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.ContainsRune(w, 's'):
			return true
		case !strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "+"):
			return false
		}
	}
	return true
}

// innerPaths are the files that the strings a shell's -c, eval or a $(...) runs write or remove, read as
// the line itself is read (shellPaths), so a target inside such a string is a target of the line.
func innerPaths(parsed shellwords.Result, depth int) []string {
	if depth == 0 {
		return nil
	}
	var out []string
	visit := func(line string) {
		if inner, err := shellwords.Split(line); err == nil {
			out = append(out, shellPaths(inner.Segments)...)
			out = append(out, innerPaths(inner, depth-1)...)
		}
	}
	for _, seg := range parsed.Segments {
		simple, _ := simpleCommand(seg.Words)
		if words := simple[assignmentsEnd(simple):]; len(words) > 0 {
			for _, line := range innerLines(words, segmentInput(seg)) {
				visit(line)
			}
		}
	}
	for _, body := range parsed.Substitutions {
		visit(body)
	}
	return out
}
