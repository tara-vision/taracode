package classify

import "strings"

// Git classifies the arguments after "git". Leading global options (-C dir, -c k=v, --no-pager,
// --git-dir=..., --work-tree=...) are skipped.
func Git(tokens []string) Result {
	tokens = stripGitGlobals(tokens)
	if len(tokens) == 0 {
		return read("")
	}
	verb, rest := tokens[0], tokens[1:]
	switch verb {
	case "status", "diff", "log", "show", "blame", "rev-parse", "ls-files", "ls-tree", "cat-file", "describe",
		"reflog", "shortlog", "grep", "rev-list", "name-rev", "check-ignore", "diff-tree", "show-ref",
		"for-each-ref", "count-objects", "version", "help", "var", "merge-base", "cherry", "whatchanged":
		return read(verb)
	case "branch":
		if hasFlag(rest, "-d", "-D", "-m", "-M", "-c", "-C", "-u", "--set-upstream-to", "--unset-upstream", "-f",
			"--force", "--edit-description") {
			return mutate(verb, "git branch with a create, move or delete option changes the repository")
		}
		if len(positionals(rest, "--contains", "--no-contains", "--merged", "--no-merged", "--sort", "--format")) == 0 ||
			hasFlag(rest, "-l", "--list", "-a", "-r", "--show-current") {
			return read(verb)
		}
		return mutate(verb, "git branch <name> creates a branch")
	case "tag":
		if len(positionals(rest, "--sort", "--format", "--contains", "--points-at")) == 0 || hasFlag(rest, "-l", "--list") {
			return read(verb)
		}
		return mutate(verb, "git tag <name> creates a tag")
	case "remote":
		if len(rest) == 0 || in(rest[0], "-v", "--verbose", "show", "get-url") {
			return read(verb)
		}
		return mutate(verb, "git remote "+rest[0]+" changes remotes")
	case "config":
		if hasFlag(rest, "--get", "--get-all", "--get-regexp", "--list", "-l") {
			return read(verb)
		}
		return mutate(verb, "git config without --get or --list writes configuration")
	case "stash":
		if in(first(rest), "list", "show") {
			return read(verb)
		}
		return mutate(verb, "git stash changes the working tree")
	case "worktree", "submodule", "notes", "bisect":
		if in(first(rest), "list", "status", "show", "log", "visualize") {
			return read(verb)
		}
		return mutate(verb, "git "+verb+" "+first(rest)+" changes the repository")
	}
	return mutate(verb, "git "+verb+" changes the repository")
}

func stripGitGlobals(tokens []string) []string {
	for len(tokens) > 0 {
		switch {
		case in(tokens[0], "-C", "-c") && len(tokens) > 1:
			tokens = tokens[2:]
		case strings.HasPrefix(tokens[0], "--git-dir"), strings.HasPrefix(tokens[0], "--work-tree"),
			in(tokens[0], "--no-pager", "-P", "--paginate", "-p", "--no-optional-locks"):
			tokens = tokens[1:]
		default:
			return tokens
		}
	}
	return tokens
}
