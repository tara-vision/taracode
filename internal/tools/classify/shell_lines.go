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
			for _, inner := range append(innerLines(words), scriptInput(seg, words)...) {
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

// scriptInput is the script a segment's here-documents and here-strings hand a shell: their bodies when the
// segment pipes its output into the next command, which may run it, and when its program is a shell without -c
// that reads its script from its input (sh, sh -s, sh -, sh /dev/stdin, source /dev/stdin).
func scriptInput(seg shellwords.Segment, words []string) []string {
	input := segmentInput(seg)
	if len(input) == 0 || seg.Piped {
		return input
	}
	w := unwrap(words)
	if len(w.words) == 0 {
		return nil
	}
	prog, rest := path.Base(w.words[0]), w.words[1:]
	switch {
	case shellNames[prog] && len(innerLines(w.words)) == 0 && scriptOnInput(rest):
		return input
	case (prog == "source" || prog == ".") && len(rest) > 0 && stdinFile(rest[0]):
		return input
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
// script file or names the input as one, or a -s says so. -o and +o take an option's name.
func scriptOnInput(rest []string) bool {
	for i := 0; i < len(rest); i++ {
		switch w := rest[i]; {
		case w == "-o" || w == "+o":
			i++
		case strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.ContainsRune(w, 's'):
			return true
		case !strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "+"):
			return stdinFile(w)
		}
	}
	return true
}

// stdinFile reports a script operand that names the standard input.
func stdinFile(w string) bool {
	return in(w, "-", "/dev/stdin", "/dev/fd/0", "/proc/self/fd/0")
}

// bodyLines are the deny lines of every here-document body on the line, which a line the classifier cannot see
// into may run (a script it writes and then runs, a program on an interpreter's input).
func bodyLines(parsed shellwords.Result) []string {
	var out []string
	for _, seg := range parsed.Segments {
		for _, body := range seg.Heredocs {
			out = append(out, linesOf(body, innerDepth-1)...)
		}
	}
	return out
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
			for _, line := range append(innerLines(words), scriptInput(seg, words)...) {
				visit(line)
			}
		}
	}
	for _, body := range parsed.Substitutions {
		visit(body)
	}
	return out
}
