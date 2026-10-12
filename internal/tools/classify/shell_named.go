package classify

import (
	"path"
	"slices"
	"strings"
)

// namedWriters maps the programs that act on some of the paths they name by name only to those paths: the
// destination of a copy, move or link, the start points and names of a find that its tests narrow, where tar and
// unzip extract, and what git checks out, restores, moves or removes. The policy matches them against its
// protected patterns but does not look inside a directory among them.
var namedWriters = map[string]func(rest []string) []string{
	"cp": destinationNamed, "install": destinationNamed, "mv": destinationNamed, "ln": destinationNamed,
	"find": findNamed, "tar": tarNamed, "unzip": unzipNamed, "git": gitNamed,
}

// copyValued are the options of cp, install, mv and ln that take the next word as their value.
var copyValued = []string{"-t", "--target-directory", "-S", "--suffix", "-m", "--mode", "-o", "--owner", "-g",
	"--group"}

// destination splits the operands of cp, install, mv and ln into the destination (the -t directory or the last
// operand) and the sources. dest is empty when the command names one operand.
func destination(r []string) (dest string, sources []string) {
	ops := operands(r, copyValued...)
	if dir := flagValue(r, "-t", "--target-directory"); dir != "" {
		return dir, ops
	}
	if len(ops) < 2 {
		return "", ops
	}
	return ops[len(ops)-1], ops[:len(ops)-1]
}

// into is where each source lands in a destination directory: under it, by the source's base name.
func into(dest string, sources []string) []string {
	var out []string
	for _, src := range sources {
		out = append(out, path.Join(dest, path.Base(src)))
	}
	return out
}

func destinationNamed(r []string) []string {
	dest, _ := destination(r)
	return []string{dest}
}

// landingWriters maps cp, install, mv and ln to where each source lands when the destination is a directory
// (into); the caller keeps the landings whose directory exists, since a destination that is a file is written
// itself, by its name.
var landingWriters = map[string]func(rest []string) []string{
	"cp": landings, "install": landings, "mv": landings, "ln": landings,
}

func landings(r []string) []string {
	dest, sources := destination(r)
	if dest == "" {
		return nil
	}
	return into(dest, sources)
}

// copyWritten is the destination of cp and install when they merge into it (mergedInto); otherwise the
// destination is named (destinationNamed) and each source's landing checked (landings).
func copyWritten(r []string) []string {
	dest, sources := destination(r)
	if dest == "" {
		return nil
	}
	return mergedInto(r, dest, sources)
}

// moveWritten are the files mv removes, the sources, and its destination when it merges into it.
func moveWritten(r []string) []string {
	dest, sources := destination(r)
	if dest == "" {
		return sources
	}
	return slices.Concat(sources, mergedInto(r, dest, sources))
}

// mergedInto is the destination when a copy or move writes into it rather than under it by a source's base name:
// with -T, and for a source with a trailing slash, whose contents BSD cp copies.
func mergedInto(r []string, dest string, sources []string) []string {
	merges := hasFlag(r, "-T", "--no-target-directory") || slices.ContainsFunc(sources, func(s string) bool {
		return strings.HasSuffix(s, "/")
	})
	if !merges {
		return nil
	}
	return []string{dest}
}

// linkWritten are what ln links to: a link reaches what it points to, so the sources are targets (a relative
// source of a symbolic link is read from the link's directory). With one operand the link is the source's base
// name in the working directory; with a destination it is named, or lands under it (landings).
func linkWritten(r []string) []string {
	dest, sources := destination(r)
	if dest == "" {
		return slices.Concat(sources, into(".", sources))
	}
	dir := path.Dir(dest)
	if flagValue(r, "-t", "--target-directory") != "" {
		dir = dest
	}
	var out []string
	for _, src := range sources {
		if hasFlag(r, "-s", "--symbolic") && relativePath(src) {
			src = path.Join(dir, src)
		}
		out = append(out, src)
	}
	return out
}

// tarNamed is the directory tar extracts into (-C, --directory), when it extracts.
func tarNamed(r []string) []string {
	bundle := len(r) > 0 && !strings.HasPrefix(r[0], "-") && strings.ContainsRune(r[0], 'x') // tar xzf
	if !bundle && !hasFlag(r, "-x", "--extract", "--get") {
		return nil
	}
	return []string{flagValue(r, "-C", "--directory")}
}

func unzipNamed(r []string) []string { return []string{flagValue(r, "-d")} }

// gitNamed are the paths git rm, mv, checkout and restore act on: their operands, under the directory git's -C
// options move it to.
func gitNamed(r []string) []string {
	dir := ""
	for i := 0; i+1 < len(r) && in(r[i], "-C", "-c"); i += 2 {
		if r[i] == "-C" {
			dir = joinDir(dir, r[i+1])
		}
	}
	tokens, _, ok := stripGitGlobals(r)
	if !ok || len(tokens) == 0 || !in(tokens[0], "rm", "mv", "checkout", "restore") {
		return nil
	}
	return inDir(dir, operands(tokens[1:]))
}

// chdirOf is the directory the wrappers in front of a program move it to (env -C, sudo -D, --chdir), from the
// words of the wrappers, the first wrapper included.
func chdirOf(wrappers []string) string {
	dir, prog := "", ""
	for i, w := range wrappers {
		switch {
		case wrapperPrograms[path.Base(w)]:
			prog = path.Base(w)
		case (prog == "env" && w == "-C" || prog == "sudo" && w == "-D" || w == "--chdir") && i+1 < len(wrappers):
			dir = joinDir(dir, wrappers[i+1])
		case strings.HasPrefix(w, "--chdir="):
			dir = joinDir(dir, strings.TrimPrefix(w, "--chdir="))
		}
	}
	return dir
}

// joinDir is next read from dir: next itself when it is not relative or dir is empty.
func joinDir(dir, next string) string {
	if dir == "" || !relativePath(next) {
		return next
	}
	return path.Join(dir, next)
}

// inDir reads each relative path from dir.
func inDir(dir string, paths []string) []string {
	if dir == "" {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, joinDir(dir, p))
	}
	return out
}
