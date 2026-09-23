package classify

import "strings"

// kubectlNeverRead are the verbs that run in or connect to a container whatever their flags say:
// in kubectl exec web -- ls --dry-run=client the --dry-run belongs to ls, and ls runs.
var kubectlNeverRead = []string{"exec", "cp", "attach", "debug", "port-forward", "proxy"}

// Kubectl classifies a verb with its remaining arguments. A last --dry-run that is bare, client or
// server is a read, except for the verbs in kubectlNeverRead. Tokens after a lone "--" are the
// command a container runs or positional arguments, never kubectl's flags, so they are not read.
// Global options before the verb (kubectl -n kube-system get pods) are skipped with their values; an
// option there that is not a known global makes the command a mutation.
func Kubectl(verb string, tokens []string) Result {
	if looksLikeFlag(verb) {
		v, rest, ok := kubectlGlobals.splitVerb(append([]string{verb}, tokens...))
		if !ok {
			return unknownGlobal("kubectl", v)
		}
		verb, tokens = v, rest
	}
	verb = strings.ToLower(strings.TrimSpace(verb))
	tokens = beforeDoubleDash(tokens)
	if res, ok := kubectlAlwaysMutates(verb, tokens); ok {
		return res
	}
	if dry, set := lastDryRun(tokens); set && in(dry, "", "client", "server") {
		return read(verb)
	}
	switch verb {
	case "get", "describe", "logs", "events", "top", "explain", "api-resources", "api-versions", "version", "diff",
		"cluster-info", "wait", "options", "help", "kustomize":
		return read(verb)
	case "auth":
		if kubectlSubcommand(tokens) == "can-i" {
			return read("auth can-i")
		}
	case "config":
		sub := kubectlSubcommand(tokens)
		if in(sub, "view", "current-context", "get-contexts", "get-clusters", "get-users") {
			return read("config " + sub)
		}
		return mutate("config "+sub, "kubectl config "+sub+" changes the kubeconfig")
	case "rollout":
		sub := kubectlSubcommand(tokens)
		if in(sub, "status", "history") {
			return read("rollout " + sub)
		}
		return mutate("rollout "+sub, "kubectl rollout "+sub+" changes workloads")
	}
	return mutate(verb, "kubectl "+verb+" changes the cluster")
}

// kubectlSubcommand is the first word after the verb that is not a global option or its value:
// kubectl rollout -n apps status deploy/web reads. An unknown option there is returned as the word,
// which no read list names.
func kubectlSubcommand(tokens []string) string {
	i, _ := kubectlGlobals.skip(tokens)
	return first(tokens[i:])
}

// kubectlAlwaysMutates catches the invocations no --dry-run makes a read: a never-read verb,
// kustomize with exec plugins or helm, which run programs, and cluster-info dump into a directory,
// which writes files.
func kubectlAlwaysMutates(verb string, tokens []string) (Result, bool) {
	if in(verb, kubectlNeverRead...) {
		return mutate(verb, "kubectl "+verb+" runs in or connects to a container, whatever its flags say"), true
	}
	switch {
	case verb == "kustomize" && hasFlag(tokens, "--enable-exec", "--enable-alpha-plugins", "--enable-helm",
		"--helm-command"):
		return mutate(verb, "kubectl kustomize with exec plugins or helm enabled runs programs"), true
	case verb == "cluster-info" && hasFlag(tokens, "--output-directory"):
		return mutate(verb, "kubectl cluster-info dump --output-directory writes files"), true
	}
	return Result{}, false
}
