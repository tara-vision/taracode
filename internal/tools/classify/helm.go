package classify

// Helm classifies the arguments after "helm". --dry-run makes any verb a read.
func Helm(tokens []string) Result {
	if len(tokens) == 0 {
		return read("")
	}
	verb, rest := tokens[0], tokens[1:]
	if hasFlag(rest, "--dry-run") {
		return read(verb)
	}
	switch verb {
	case "list", "ls", "status", "get", "history", "hist", "show", "inspect", "template", "lint", "diff",
		"version", "env", "search", "verify", "completion", "help":
		return read(verb)
	case "repo", "dependency", "dep", "plugin":
		if in(first(rest), "list", "ls") {
			return read(verb)
		}
		return mutate(verb, "helm "+verb+" "+first(rest)+" changes local helm state")
	}
	return mutate(verb, "helm "+verb+" changes releases")
}
