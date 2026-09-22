package classify

// composeValueFlags are docker compose options that take a value before the subcommand.
var composeValueFlags = []string{"-f", "--file", "-p", "--project-name", "--env-file", "--profile",
	"--project-directory", "--parallel", "--progress"}

// Docker classifies the arguments after "docker" (or podman).
func Docker(tokens []string) Result {
	if len(tokens) == 0 {
		return read("")
	}
	verb, rest := tokens[0], tokens[1:]
	sub := first(positionals(rest, composeValueFlags...))
	switch verb {
	case "ps", "images", "logs", "inspect", "top", "port", "diff", "history", "version", "info", "search", "stats",
		"events":
		return read(verb)
	case "image":
		if in(sub, "ls", "list", "inspect", "history") {
			return read(verb)
		}
	case "container":
		if in(sub, "ls", "list", "ps", "inspect", "logs", "top", "stats", "port", "diff") {
			return read(verb)
		}
	case "network", "volume", "context", "node", "service", "stack", "secret", "config", "plugin", "trust":
		if in(sub, "ls", "list", "inspect", "logs", "ps", "show") {
			return read(verb)
		}
	case "system":
		if in(sub, "df", "info", "events") {
			return read(verb)
		}
	case "compose":
		if in(sub, "ps", "config", "logs", "version", "ls", "images", "top", "events", "port") {
			return read(verb)
		}
	case "manifest":
		if sub == "inspect" {
			return read(verb)
		}
	case "buildx":
		if in(sub, "ls", "version", "inspect") {
			return read(verb)
		}
	}
	if sub != "" {
		return mutate(verb, "docker "+verb+" "+sub+" changes containers, images or local state")
	}
	return mutate(verb, "docker "+verb+" changes containers, images or local state")
}
