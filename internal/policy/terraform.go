package policy

import (
	"path"
	"strconv"
	"strings"
)

// terraformPrograms are the programs the canonical terraform form reads as terraform, by base name.
var terraformPrograms = map[string]bool{"terraform": true, "tofu": true, "terragrunt": true}

// terragruntRunWords are the words terragrunt puts before the terraform verb: run-all destroy, run --all destroy.
var terragruntRunWords = map[string]bool{"run-all": true, "run": true}

// canonicalTerraformCommand spells the terraform commands of a collapsed command line one way for the deny
// patterns, next to the kubectl form: terraform, tofu and terragrunt (a path to one is its name) read as
// terraform, the global options before the verb (-chdir=infra) and terragrunt's run words are dropped, and
// apply or plan with a true destroy flag reads as destroy, the flag dropped. So tofu apply -auto-approve
// -destroy and terragrunt run-all destroy both read as terraform destroy ..., which the shipped pattern
// terraform destroy* refuses. Words before the first terraform program, and a line without one, stay as
// written.
func canonicalTerraformCommand(command string) string {
	words := strings.Fields(command)
	program, verb, ok := terraformVerb(words)
	if !ok {
		return command
	}
	out := append(append([]string{}, words[:program]...), "terraform")
	if verb >= len(words) {
		return strings.Join(out, " ")
	}
	rest := words[verb+1:]
	if (words[verb] == "apply" || words[verb] == "plan") && destroyFlag(rest) {
		return strings.Join(append(append(out, "destroy"), withoutDestroyFlag(rest)...), " ")
	}
	return strings.Join(append(out, words[verb:]...), " ")
}

// terraformVerb finds the first terraform program on the line and the index of its verb, past the global
// options and terragrunt's run words; verb is len(words) when nothing follows the program.
func terraformVerb(words []string) (program, verb int, ok bool) {
	for i, w := range words {
		name := path.Base(w)
		if !terraformPrograms[name] {
			continue
		}
		j := i + 1
		for j < len(words) && (strings.HasPrefix(words[j], "-") || (name == "terragrunt" && terragruntRunWords[words[j]])) {
			j++
		}
		return i, j, true
	}
	return 0, 0, false
}

// destroyFlag reads terraform's -destroy the way the classifier reads terraform's booleans (goBoolFlag in
// internal/tools/classify, which this package cannot import): -destroy and --destroy set it, -destroy=v sets
// it to v, the last occurrence wins, and a value strconv.ParseBool rejects counts as false.
func destroyFlag(words []string) bool {
	value := false
	for _, w := range words {
		isFlag, v, hasValue := destroyWord(w)
		if !isFlag {
			continue
		}
		value = true
		if hasValue {
			b, err := strconv.ParseBool(v)
			value = err == nil && b
		}
	}
	return value
}

// destroyWord tells whether a word is the destroy flag, and its value when one is written after =.
func destroyWord(w string) (isFlag bool, value string, hasValue bool) {
	if !strings.HasPrefix(w, "-") {
		return false, "", false
	}
	name, value, hasValue := strings.Cut(strings.TrimLeft(w, "-"), "=")
	return name == "destroy", value, hasValue
}

// withoutDestroyFlag drops the destroy flag's words.
func withoutDestroyFlag(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if isFlag, _, _ := destroyWord(w); !isFlag {
			out = append(out, w)
		}
	}
	return out
}
