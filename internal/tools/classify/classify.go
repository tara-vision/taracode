// Package classify decides whether one invocation of git, kubectl, helm, terraform, docker, a cloud
// CLI or an arbitrary shell command reads or mutates, and names the verb and the reason. It never
// runs anything. The tools package calls it for the dedicated tools and, through Shell, for the
// shell tool; both paths share the same tables.
package classify

import (
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

// flagValue returns the value of the first flag in names, written --name=value or --name value.
func flagValue(tokens []string, names ...string) string {
	for i, t := range tokens {
		for _, n := range names {
			if strings.HasPrefix(t, n+"=") {
				return strings.TrimPrefix(t, n+"=")
			}
			if t == n && i+1 < len(tokens) {
				return tokens[i+1]
			}
		}
	}
	return ""
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
