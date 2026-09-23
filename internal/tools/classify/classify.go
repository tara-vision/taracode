// Package classify decides whether one invocation of git, kubectl, helm, terraform, docker, a cloud
// CLI or an arbitrary shell command reads or mutates, and names the verb and the reason. It never
// runs anything. The tools package calls it for the dedicated tools and, through Shell, for the
// shell tool; both paths share the same tables.
package classify

import (
	"strconv"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
)

// Result is one classification.
type Result struct {
	Classification policy.Classification
	Verb           string
	Reason         string // set on mutate; what the model and the user read
}

func read(verb string) Result { return Result{Classification: policy.Read, Verb: verb} }

func mutate(verb, reason string) Result {
	return Result{Classification: policy.Mutate, Verb: verb, Reason: reason}
}

func first(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

// hasFlag reports whether any token equals one of names or starts with name= (or is a short flag
// cluster such as -la containing a one-letter name).
func hasFlag(tokens []string, names ...string) bool {
	for _, t := range tokens {
		for _, n := range names {
			if t == n || strings.HasPrefix(t, n+"=") {
				return true
			}
			if len(n) == 2 && strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") &&
				strings.ContainsRune(t[1:], rune(n[1])) {
				return true
			}
		}
	}
	return false
}

// flagValue returns the value of the first flag in names, written --name=value or --name value. A
// token that looks like a flag is never taken as a bare flag's value (it is the next flag, not this
// flag's argument), so a bare flag followed by another flag with no value in between reports no
// value instead of swallowing that next flag; a lone "-" is not flag-shaped, so the conventional
// stdin/stdout placeholder still counts as a value.
func flagValue(tokens []string, names ...string) string {
	for i, t := range tokens {
		for _, n := range names {
			if strings.HasPrefix(t, n+"=") {
				return strings.TrimPrefix(t, n+"=")
			}
			if t == n && i+1 < len(tokens) && !looksLikeFlag(tokens[i+1]) {
				return tokens[i+1]
			}
		}
	}
	return ""
}

// looksLikeFlag reports whether s is shaped like a flag rather than a value: more than one character
// and starting with "-". A lone "-" is excluded, since by convention it means "stdin" or "stdout",
// not an option (e.g. wget -O -).
func looksLikeFlag(s string) bool {
	return len(s) > 1 && strings.HasPrefix(s, "-")
}

// positionals returns the tokens that are not flags, skipping the value that follows any of the
// flags in valueFlags (flags written --name=value carry their value already).
func positionals(tokens []string, valueFlags ...string) []string {
	var out []string
	skip := false
	for _, t := range tokens {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(t, "-") {
			for _, vf := range valueFlags {
				if t == vf {
					skip = true
				}
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

func in(value string, set ...string) bool {
	for _, s := range set {
		if value == s {
			return true
		}
	}
	return false
}

func hasPrefixIn(value string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}

// operands returns the tokens that are not flags, skipping the value that follows any flag in
// valueFlags. Unlike positionals, a lone "-" is an operand: standard input or standard output.
func operands(tokens []string, valueFlags ...string) []string {
	var out []string
	skip := false
	for _, t := range tokens {
		if skip {
			skip = false
			continue
		}
		if looksLikeFlag(t) {
			skip = in(t, valueFlags...)
			continue
		}
		out = append(out, t)
	}
	return out
}

// hasGNUFlag reports whether a token is one of the long names (--name, --name=value) or an
// abbreviation of one: GNU getopt_long, and with it sort, sed, wget, journalctl, tar and curl 7,
// accepts any unambiguous prefix of a long option, so --in is --in-place to GNU sed. own lists the
// program's other long options that are prefixes of a name (curl's --cookie of --cookie-jar):
// written in full they are that option, not an abbreviation.
func hasGNUFlag(tokens, own []string, names ...string) bool {
	for _, t := range tokens {
		name, _, _ := strings.Cut(t, "=")
		if len(name) < 3 || !strings.HasPrefix(name, "--") || in(name, own...) {
			continue
		}
		for _, n := range names {
			if strings.HasPrefix(n, name) {
				return true
			}
		}
	}
	return false
}

// gnuFlagValue is flagValue for one long option written in full or abbreviated, as getopt_long
// reads it: --name=value or --name value.
func gnuFlagValue(tokens []string, name string) string {
	for i, t := range tokens {
		n, value, hasValue := strings.Cut(t, "=")
		if len(n) < 3 || !strings.HasPrefix(n, "--") || !strings.HasPrefix(name, n) {
			continue
		}
		if hasValue {
			return value
		}
		if i+1 < len(tokens) {
			return tokens[i+1]
		}
	}
	return ""
}

// shortFlag reports whether a single-dash token such as -o, -ro or -Pi sets one of the letters in
// flags. A cluster is read letter by letter up to the first letter in valued, whose value is the
// rest of the token: -ojson is -o with the value json, not -o -j -s -o -n.
func shortFlag(tokens []string, flags, valued string) bool {
	for _, t := range tokens {
		if len(t) < 2 || t[0] != '-' || t[1] == '-' {
			continue
		}
		for _, c := range t[1:] {
			if strings.ContainsRune(flags, c) {
				return true
			}
			if strings.ContainsRune(valued, c) {
				break
			}
		}
	}
	return false
}

// shortValue returns the value of the short flag letter in a cluster read as shortFlag reads it:
// the rest of the token (-XPOST, -sXPOST) or, when the letter ends it, the next token (-X POST).
func shortValue(tokens []string, letter rune, valued string) string {
	for i, t := range tokens {
		if len(t) < 2 || t[0] != '-' || t[1] == '-' {
			continue
		}
		for j, c := range t[1:] {
			if c == letter {
				if rest := t[j+2:]; rest != "" {
					return rest
				}
				if i+1 < len(tokens) {
					return tokens[i+1]
				}
				return ""
			}
			if strings.ContainsRune(valued, c) {
				break
			}
		}
	}
	return ""
}

// lastDryRun returns the value of the last --dry-run on the line ("" when it is bare) and whether
// there is one. kubectl and helm apply the last occurrence, and a bare --dry-run never takes the
// next word as its value (it is a client dry run; the word is a positional argument).
func lastDryRun(tokens []string) (value string, set bool) {
	for _, t := range tokens {
		if t == "--dry-run" {
			value, set = "", true
		} else if v, ok := strings.CutPrefix(t, "--dry-run="); ok {
			value, set = v, true
		}
	}
	return value, set
}

// beforeDoubleDash returns the tokens before a lone "--": what follows it is positional (a release
// name, the command kubectl exec runs), never the program's own flags.
func beforeDoubleDash(tokens []string) []string {
	for i, t := range tokens {
		if t == "--" {
			return tokens[:i]
		}
	}
	return tokens
}

// goFlagSet reports whether a Go flag-package option is present: -name, --name, -name=v or --name=v.
func goFlagSet(tokens []string, name string) bool {
	for _, t := range tokens {
		if n, ok := goFlagName(t); ok && n == name {
			return true
		}
	}
	return false
}

// goBoolFlag reads a Go flag-package boolean the way terraform does: -name and --name set it,
// -name=v and --name=v set it to v, the last occurrence wins, and a value strconv.ParseBool rejects
// counts as false (terraform refuses the command).
func goBoolFlag(tokens []string, name string) (value, set bool) {
	for _, t := range tokens {
		n, ok := goFlagName(t)
		if !ok || n != name {
			continue
		}
		set, value = true, true
		if _, v, hasValue := strings.Cut(t, "="); hasValue {
			b, err := strconv.ParseBool(v)
			value = err == nil && b
		}
	}
	return value, set
}

// goFlagName is the option name of a Go flag-package token (-name, --name, with or without =value).
func goFlagName(t string) (string, bool) {
	if !strings.HasPrefix(t, "-") {
		return "", false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(t[1:], "-"), "=")
	return name, name != ""
}
