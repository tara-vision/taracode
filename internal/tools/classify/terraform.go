package classify

import "strings"

// Terraform classifies a command with its arguments. plan is a read (the tool writes the plan file
// into a session temp file) unless it is told to write a file itself; init is a read only with
// -backend=false. Options follow Go's flag package: -name or --name, and the last one wins.
func Terraform(command string, tokens []string) Result {
	command = strings.ToLower(strings.TrimSpace(command))
	switch command {
	case "validate", "show", "output", "graph", "version", "metadata", "help":
		return read(command)
	case "plan":
		if goFlagSet(tokens, "out") || goFlagSet(tokens, "generate-config-out") {
			return mutate(command, "terraform plan -out and -generate-config-out write files")
		}
		return read(command)
	case "fmt":
		if check, _ := goBoolFlag(tokens, "check"); check {
			return read(command)
		}
		if write, set := goBoolFlag(tokens, "write"); set && !write {
			return read(command)
		}
		return mutate(command, "terraform fmt without -check or -write=false rewrites files")
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
		if backend, set := goBoolFlag(tokens, "backend"); set && !backend {
			return read(command)
		}
		return mutate(command, "terraform init without -backend=false may configure or migrate state")
	}
	return mutate(command, "terraform "+command+" changes infrastructure or state")
}
