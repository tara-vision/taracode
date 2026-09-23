package classify

import "strings"

// kubectlNeverRead are the verbs that run in or connect to a container whatever their flags say:
// in kubectl exec web -- ls --dry-run=client the --dry-run belongs to ls, and ls runs.
var kubectlNeverRead = []string{"exec", "cp", "attach", "debug", "port-forward", "proxy"}

// Kubectl classifies a verb with its remaining arguments. A last --dry-run that is bare, client or
// server is a read, except for the verbs in kubectlNeverRead. Tokens after a lone "--" are the
// command a container runs or positional arguments, never kubectl's flags, so they are not read.
func Kubectl(verb string, tokens []string) Result {
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
		if first(tokens) == "can-i" {
			return read("auth can-i")
		}
	case "config":
		if in(first(tokens), "view", "current-context", "get-contexts", "get-clusters", "get-users") {
			return read("config " + first(tokens))
		}
		return mutate("config "+first(tokens), "kubectl config "+first(tokens)+" changes the kubeconfig")
	case "rollout":
		if in(first(tokens), "status", "history") {
			return read("rollout " + first(tokens))
		}
		return mutate("rollout "+first(tokens), "kubectl rollout "+first(tokens)+" changes workloads")
	}
	return mutate(verb, "kubectl "+verb+" changes the cluster")
}

// kubectlAlwaysMutates catches the invocations no --dry-run makes a read: a never-read verb (also
// after leading global flags, kubectl -n apps exec ...), kustomize with exec plugins or helm, which
// run programs, and cluster-info dump into a directory, which writes files.
func kubectlAlwaysMutates(verb string, tokens []string) (Result, bool) {
	if in(verb, kubectlNeverRead...) {
		return mutate(verb, "kubectl "+verb+" runs in or connects to a container, whatever its flags say"), true
	}
	if looksLikeFlag(verb) {
		for _, t := range tokens {
			if in(t, kubectlNeverRead...) {
				return mutate(t, "kubectl "+t+" runs in or connects to a container, whatever its flags say"), true
			}
		}
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

// KubeTargets reads the context and namespace named on the command line; "*" means all namespaces.
// Flags after a lone "--" belong to the command a container runs, not to kubectl.
func KubeTargets(tokens []string) (context, namespace string) {
	tokens = beforeDoubleDash(tokens)
	context = flagValue(tokens, "--context")
	namespace = flagValue(tokens, "-n", "--namespace")
	if hasFlag(tokens, "-A", "--all-namespaces") {
		namespace = "*"
	}
	return context, namespace
}
