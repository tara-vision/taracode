package classify

import "strings"

// Terraform classifies a command with its arguments. plan is a read (the tool writes the plan file
// into a session temp file); init is a read only with -backend=false.
func Terraform(command string, tokens []string) Result {
	command = strings.ToLower(strings.TrimSpace(command))
	switch command {
	case "validate", "show", "output", "graph", "version", "plan", "metadata", "help":
		return read(command)
	case "fmt":
		if hasFlag(tokens, "-check", "-diff") {
			return read(command)
		}
		return mutate(command, "terraform fmt without -check rewrites files")
	case "state":
		if in(first(tokens), "list", "show", "pull") {
			return read("state " + first(tokens))
		}
		return mutate("state "+first(tokens), "terraform state "+first(tokens)+" changes the state")
	case "workspace":
		if in(first(tokens), "list", "show", "") {
			return read("workspace " + first(tokens))
		}
		return mutate("workspace "+first(tokens), "terraform workspace "+first(tokens)+" changes workspaces")
	case "providers":
		if in(first(tokens), "", "schema") {
			return read(command)
		}
		return mutate(command, "terraform providers "+first(tokens)+" writes the lock file or a mirror")
	case "init":
		if hasFlag(tokens, "-backend=false") {
			return read(command)
		}
		return mutate(command, "terraform init without -backend=false may configure or migrate state")
	}
	return mutate(command, "terraform "+command+" changes infrastructure or state")
}
