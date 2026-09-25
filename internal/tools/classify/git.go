package classify

import "strings"

// gitReadSafeConfig are the -c keys (or key prefixes, ending in a dot) that only change how a read
// looks. Any other key can make a read run a program (diff.external, core.fsmonitor, gpg.program).
var gitReadSafeConfig = []string{"color.", "core.quotepath", "core.abbrev", "log.date", "log.decorate",
	"diff.noprefix", "diff.renames", "column.ui"}

// Git classifies the arguments after "git". Leading global options (-C dir, -c k=v with a read-safe
// key, --no-pager, --git-dir=..., --work-tree=...) are skipped; a -c with any other key is a
// mutation.
func Git(tokens []string) Result {
	tokens, res, ok := stripGitGlobals(tokens)
	if !ok {
		return res
	}
	if len(tokens) == 0 {
		return read("")
	}
	verb, rest := tokens[0], tokens[1:]
	switch verb {
	case "status", "diff", "log", "show", "blame", "rev-parse", "ls-files", "ls-tree", "cat-file", "describe",
		"reflog", "shortlog", "grep", "rev-list", "name-rev", "check-ignore", "diff-tree", "show-ref",
		"for-each-ref", "count-objects", "version", "help", "var", "merge-base", "cherry", "whatchanged":
		return gitReadVerb(verb, rest)
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
		return gitConfigResult(verb, rest)
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

// gitConfigResult classifies "git config": --get, --get-all, --get-regexp, --list, -l or the get and
// list subcommands read; --unset, --add, --replace-all, --rename-section, --remove-section, -e,
// --edit or a set, unset, edit, rename-section or remove-section subcommand write; a single
// remaining positional is the key of a plain read, and anything else (a key and a value) writes it.
// The values of --file, -f, --blob, --type and --default are skipped; --worktree, like --local, takes
// none, so the word after it is a key (the value of an option not listed counts as a positional,
// which fails closed).
func gitConfigResult(verb string, rest []string) Result {
	if hasFlag(rest, "--get", "--get-all", "--get-regexp", "--list", "-l") || in(first(rest), "get", "list") {
		return read(verb)
	}
	if hasFlag(rest, "--unset", "--unset-all", "--add", "--replace-all", "--rename-section", "--remove-section",
		"-e", "--edit") || in(first(rest), "set", "unset", "edit", "rename-section", "remove-section") {
		return mutate(verb, "git config with --unset, --add, --edit or a set subcommand writes configuration")
	}
	if len(positionals(rest, "--file", "-f", "--blob", "--type", "--default")) == 1 {
		return read(verb) // git config <key> reads the key
	}
	return mutate(verb, "git config <key> <value> writes configuration")
}

// gitReadVerb catches the write forms of the read verbs: reflog expire and delete prune the
// reflog, --output writes the diff or log to a file, and grep -O runs a program on the matches.
func gitReadVerb(verb string, rest []string) Result {
	switch {
	case verb == "reflog" && in(first(rest), "expire", "delete"):
		return mutate(verb, "git reflog "+first(rest)+" changes the reflog")
	case hasGNUFlag(rest, nil, "--output"):
		return mutate(verb, "git "+verb+" --output writes a file")
	case verb == "grep" && (hasGNUFlag(rest, nil, "--open-files-in-pager") || shortFlag(rest, "O", "ABCefm")):
		return mutate(verb, "git grep -O runs a program on the matching files")
	}
	return read(verb)
}

// stripGitGlobals drops the leading global options. ok is false, with the mutate result, when a -c
// sets a key outside gitReadSafeConfig.
func stripGitGlobals(tokens []string) ([]string, Result, bool) {
	for len(tokens) > 0 {
		switch {
		case tokens[0] == "-c" && len(tokens) > 1:
			if key := gitConfigKey(tokens[1]); !gitReadSafeKey(key) {
				return nil, mutate("-c", "git -c "+key+" can make any git command run a program or write files"), false
			}
			tokens = tokens[2:]
		case tokens[0] == "-C" && len(tokens) > 1:
			tokens = tokens[2:]
		case strings.HasPrefix(tokens[0], "--git-dir"), strings.HasPrefix(tokens[0], "--work-tree"),
			in(tokens[0], "--no-pager", "-P", "--paginate", "-p", "--no-optional-locks"):
			tokens = tokens[1:]
		default:
			return tokens, Result{}, true
		}
	}
	return tokens, Result{}, true
}

// gitConfigKey is the key of a -c name=value (git matches section and key names case-insensitively).
func gitConfigKey(setting string) string {
	key, _, _ := strings.Cut(setting, "=")
	return strings.ToLower(key)
}

func gitReadSafeKey(key string) bool {
	for _, safe := range gitReadSafeConfig {
		if key == safe || strings.HasSuffix(safe, ".") && strings.HasPrefix(key, safe) {
			return true
		}
	}
	return false
}
