package classify

// Helm classifies the arguments after "helm". A last --dry-run that is bare, client, server or true
// makes any verb a read (helm 3.13 and later run for real on =false and =none), unless the command
// runs a post-renderer (a program) or writes rendered manifests to a directory. Tokens after a lone
// "--" are release names, never flags: helm uninstall web -- --dry-run uninstalls web. Global
// options before the verb (helm -n kube-system list) are skipped with their values; an option there
// that is not a known global makes the command a mutation.
func Helm(tokens []string) Result {
	verb, rest, ok := helmGlobals.splitVerb(tokens)
	if !ok {
		return unknownGlobal("helm", verb)
	}
	if verb == "" {
		return read("")
	}
	rest = beforeDoubleDash(rest)
	if hasFlag(rest, "--post-renderer") {
		return mutate(verb, "helm --post-renderer runs a program on the rendered manifests")
	}
	if hasFlag(rest, "--output-dir") {
		return mutate(verb, "helm "+verb+" --output-dir writes the rendered manifests to files")
	}
	if dry, set := lastDryRun(rest); set && in(dry, "", "client", "server", "true") {
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
