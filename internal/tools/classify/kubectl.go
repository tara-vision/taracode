package classify

import "strings"

// Kubectl classifies a verb with its remaining arguments. Any --dry-run other than none is a read.
func Kubectl(verb string, tokens []string) Result {
	verb = strings.ToLower(strings.TrimSpace(verb))
	if dry := flagValue(tokens, "--dry-run"); dry == "client" || dry == "server" ||
		(dry == "" && hasFlag(tokens, "--dry-run")) {
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

// KubeTargets reads the context and namespace named on the command line; "*" means all namespaces.
func KubeTargets(tokens []string) (context, namespace string) {
	context = flagValue(tokens, "--context")
	namespace = flagValue(tokens, "-n", "--namespace")
	if hasFlag(tokens, "-A", "--all-namespaces") {
		namespace = "*"
	}
	return context, namespace
}
