package policy

import (
	"path"
	"strconv"
	"strings"
)

// terraformPrograms are the programs the canonical terraform form reads as terraform, by base name.
var terraformPrograms = map[string]bool{"terraform": true, "tofu": true, "terragrunt": true}

// terraformVerbs are the commands of terraform and tofu. The verb of a line is the first of them after the
// program, past the global options with their values (-chdir=infra, --working-dir infra) and terragrunt's
// run words (run-all, run --all, stack run).
var terraformVerbs = map[string]bool{
	"apply": true, "console": true, "destroy": true, "env": true, "fmt": true, "force-unlock": true, "get": true,
	"graph": true, "import": true, "init": true, "login": true, "logout": true, "metadata": true, "modules": true,
	"output": true, "plan": true, "providers": true, "query": true, "refresh": true, "show": true, "state": true,
	"taint": true, "test": true, "untaint": true, "validate": true, "version": true, "workspace": true,
	// terragrunt's older spellings of run-all <verb>.
	"apply-all": true, "destroy-all": true, "plan-all": true, "validate-all": true, "output-all": true,
	"refresh-all": true,
}

// terraformVerbWord is the verb without terragrunt's -all suffix: apply-all is an apply.
func terraformVerbWord(w string) string { return strings.TrimSuffix(w, "-all") }

// canonicalTerraformCommand spells the terraform commands of a collapsed command line one way for the deny
// patterns, next to the kubectl form: terraform, tofu and terragrunt (a path to one is its name) read as
// terraform, the words between the program and its verb (global options, their values, terragrunt's run
// words) are dropped, and apply or plan with a true destroy flag reads as destroy, the flag dropped. So
// tofu apply -auto-approve -destroy, terragrunt run-all destroy and terragrunt --working-dir infra destroy
// all read as terraform destroy ..., which the shipped pattern terraform destroy* refuses. Words before
// the first terraform program, and a line without one, stay as written.
func canonicalTerraformCommand(command string) string {
	words := strings.Fields(command)
	program, verb, ok := terraformVerb(words)
	if !ok {
		return command
	}
	fromEnv := envDestroy(words[:program]) && verb < len(words) &&
		(terraformVerbWord(words[verb]) == "apply" || terraformVerbWord(words[verb]) == "plan")
	out := append(append([]string{}, words[:program]...), "terraform")
	if fromEnv {
		// The assignment that carries the destroy is consumed into the verb, so the line starts with the
		// program as the patterns expect.
		out = []string{"terraform"}
	}
	return strings.Join(append(out, rewriteDestroy(words, verb, fromEnv)...), " ")
}

// canonicalTerraformPattern spells a deny pattern the way canonicalTerraformCommand spells a line, but keeps
// the words the operator wrote between the program and the verb (an option such as -chdir=*prod*, a
// terragrunt run word), so a pattern narrower than the shipped one refuses only what it names.
func canonicalTerraformPattern(pattern string) string {
	words := strings.Fields(pattern)
	program, verb, ok := terraformVerb(words)
	if !ok {
		return pattern
	}
	out := append(append([]string{}, words[:program]...), "terraform")
	out = append(out, words[program+1:verb]...)
	return strings.Join(append(out, rewriteDestroy(words, verb, false)...), " ")
}

// rewriteDestroy is the verb and what follows it, with apply or plan under a true destroy flag (on the line,
// or set through the environment) spelled as destroy without the flag; nothing when no verb follows the
// program.
func rewriteDestroy(words []string, verb int, fromEnv bool) []string {
	if verb >= len(words) {
		return nil
	}
	rest := words[verb+1:]
	if v := terraformVerbWord(words[verb]); (v == "apply" || v == "plan") && (fromEnv || destroyFlag(rest)) {
		return append([]string{"destroy"}, withoutDestroyFlag(rest)...)
	}
	return words[verb:]
}

// envDestroy reports a true destroy flag set through terraform's environment in the words before the
// program: TF_CLI_ARGS, TF_CLI_ARGS_apply or TF_CLI_ARGS_plan (lowercased by collapse), as an assignment in
// front of the command or exported earlier on the line. A quoted value may span several words of the
// collapsed line, so every word from the assignment to the program counts as its value, each stripped of
// quotes and a trailing semicolon; a destroy among them is a destroy, which fails closed.
func envDestroy(before []string) bool {
	for i, w := range before {
		name, value, ok := strings.Cut(w, "=")
		if !ok || !strings.HasPrefix(name, "tf_cli_args") {
			continue
		}
		flags := []string{unquote(value)}
		for _, rest := range before[i+1:] {
			flags = append(flags, unquote(rest))
		}
		if destroyFlag(flags) {
			return true
		}
	}
	return false
}

// unquote strips the quotes and the trailing semicolon a collapsed word may carry.
func unquote(w string) string {
	return strings.Trim(strings.TrimRight(w, ";"), `"'`)
}

// terraformVerb finds the first terraform program on the line and the index of its verb: the first word
// after it that names a terraform command, or, when none does, the first word that is not an option or a
// terragrunt run word (an unknown verb such as terragrunt's old destroy-all stays in the line for the
// patterns to read); verb is len(words) when nothing qualifies.
func terraformVerb(words []string) (program, verb int, ok bool) {
	for i, w := range words {
		if !terraformPrograms[path.Base(w)] {
			continue
		}
		for j := i + 1; j < len(words); j++ {
			if terraformVerbs[words[j]] {
				return i, j, true
			}
		}
		j := i + 1
		for j < len(words) && (strings.HasPrefix(words[j], "-") || terragruntRunWords[words[j]]) {
			j++
		}
		return i, j, true
	}
	return 0, 0, false
}

// terragruntRunWords are the words terragrunt puts before the terraform verb: run-all, run --all, stack run.
var terragruntRunWords = map[string]bool{"run-all": true, "run": true, "stack": true}

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
