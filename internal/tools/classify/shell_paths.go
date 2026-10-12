package classify

import (
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// lineFiles are the files a line acts on: written, which it writes, moves or removes (fileWriters), so a
// directory among them is checked for the protected paths it holds; named, which it acts on by name only
// (namedWriters); landings, where a copy, move or link puts each source if its destination is a directory
// (landingWriters); and the finds it runs narrowed by name (findTargets).
type lineFiles struct {
	written, named, landings []string
	finds                    []policy.Find
}

func (f lineFiles) with(g lineFiles) lineFiles {
	return lineFiles{written: slices.Concat(f.written, g.written), named: slices.Concat(f.named, g.named),
		landings: slices.Concat(f.landings, g.landings), finds: slices.Concat(f.finds, g.finds)}
}

// shellPaths returns the files the segments act on, as written on the line: the targets of redirects into
// files and the operands fileWriters and namedWriters give, also behind sudo, env, xargs and the other wrappers
// and after the shell's reserved words (do, then, !, {, ...). A relative path is also returned joined to every
// literal directory an earlier cd or pushd of the line moved to, since the caller resolves it against the
// directory the line starts in. Paths computed at run time (variables, command substitution) are not seen.
func shellPaths(segments []shellwords.Segment) lineFiles {
	var files lineFiles
	var dirs []string
	for _, seg := range segments {
		simple, _ := simpleCommand(seg.Words)
		simple = withoutGluedBrace(simple)
		words := simple[assignmentsEnd(simple):]
		written := writtenOperands(words)
		for _, r := range seg.Redirects {
			if writesFile(r) {
				_, target, _ := strings.Cut(r, " ")
				written = append(written, target)
			}
		}
		files = files.with(lineFiles{written: underDirs(written, dirs), named: underDirs(namedOperands(words), dirs),
			landings: underDirs(operandsOf(words, landingWriters), dirs), finds: findTargets(words, dirs)})
		execs, chdir := findExecs(words)
		for _, cmd := range execs {
			files = files.with(execFiles(cmd, chdir, dirs))
		}
		if d, ok := cdTarget(words, dirs); ok {
			dirs = append(dirs, d)
		}
	}
	return files
}

// underDirs returns each path, and a relative one also joined to every directory in dirs.
func underDirs(paths, dirs []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, p)
		if relativePath(p) {
			for _, d := range dirs {
				out = append(out, path.Join(d, p))
			}
		}
	}
	return out
}

// relativePath reports a path the working directory resolves: not absolute, not under ~ and not
// starting with a variable.
func relativePath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~") && !strings.HasPrefix(p, "$")
}

// cdTarget returns the directory a cd or pushd with a literal argument moves to (past -P, -L, -e, -@
// and --), joined to the previous one when it is relative; a glob stays in it, for the caller to
// expand. ok is false for any other command and for a directory known only at run time (cd -, cd $DIR).
func cdTarget(words, dirs []string) (string, bool) {
	if len(words) == 0 || !in(words[0], "cd", "pushd") {
		return "", false
	}
	args := words[1:]
	for len(args) > 0 && in(args[0], "-P", "-L", "-e", "-@", "--") {
		args = args[1:]
	}
	if len(args) > 1 {
		return "", false
	}
	dir := "~"
	if len(args) == 1 {
		dir = args[0]
	}
	if dir == "-" || strings.ContainsAny(dir, "$`") {
		return "", false
	}
	if relativePath(dir) && len(dirs) > 0 {
		dir = path.Join(dirs[len(dirs)-1], dir)
	}
	return dir, true
}

// wrapperPrograms run the command that follows them; the file writer is looked up after them.
var wrapperPrograms = map[string]bool{
	"sudo": true, "doas": true, "env": true, "command": true, "exec": true, "nohup": true, "time": true,
	"nice": true, "timeout": true, "builtin": true, "xargs": true, "parallel": true, "stdbuf": true,
	"ionice": true, "watch": true, "busybox": true,
}

// writtenOperands returns the operands a file-writing command writes or removes.
func writtenOperands(words []string) []string { return operandsOf(words, fileWriters) }

// namedOperands returns the operands a command acts on by name only.
func namedOperands(words []string) []string { return operandsOf(words, namedWriters) }

// operandsOf returns what a table gives for a command's operands, past the wrapper programs in front of it and
// read from the directory they move it to (env -C, sudo -D).
func operandsOf(words []string, table map[string]func(rest []string) []string) []string {
	cmd, dir := behindWrappers(words, func(prog string) bool { _, ok := table[prog]; return ok })
	if cmd == nil {
		return nil
	}
	return inDir(dir, nonEmpty(table[path.Base(cmd[0])](cmd[1:])))
}

// behindWrappers returns the command a known program runs, as the words from that program on, past the wrapper
// programs in front of it, with the directory the wrappers move it to; nil when the words run no known program.
func behindWrappers(words []string, known func(prog string) bool) ([]string, string) {
	if len(words) == 0 {
		return nil, ""
	}
	if !wrapperPrograms[path.Base(words[0])] {
		if known(path.Base(words[0])) {
			return words, ""
		}
		return nil, ""
	}
	for i, w := range words[1:] {
		if known(path.Base(w)) {
			return words[i+1:], chdirOf(words[:i+1])
		}
	}
	return nil, ""
}

// fileWriters maps the programs that write, move or remove the files they name to the operands
// they write.
var fileWriters = map[string]func(rest []string) []string{
	"tee": allOperands, "rm": allOperands, "rmdir": allOperands, "unlink": allOperands,
	"shred": func(r []string) []string { return operands(r, "-n", "--iterations", "-s", "--size") },
	"touch": func(r []string) []string { return operands(r, "-d", "--date", "-t", "-r", "--reference") },
	"truncate": func(r []string) []string {
		return operands(r, "-s", "--size", "-r", "--reference")
	},
	"mv": moveWritten, "cp": cpWritten, "install": copyWritten, "ln": linkWritten, "rsync": rsyncWritten,
	"chmod": chmodTargets,
	"chown": ownerTargets, "chgrp": ownerTargets, "dd": ddOutput, "sed": sedTargets, "yq": yqTargets, "sort": sortOutput,
	"curl": curlOutputs, "wget": wgetOutputs, "find": findWritten,
}

func allOperands(r []string) []string { return operands(r) }

func nonEmpty(paths []string) []string {
	var out []string
	for _, p := range paths {
		if p != "" && p != "-" {
			out = append(out, p)
		}
	}
	return out
}

var chmodMode = regexp.MustCompile(`^([0-7]{1,4}|[ugoa]*[-+=][rwxXstugo]*(,[ugoa]*[-+=][rwxXstugo]*)*)$`)

// chmodTargets are chmod's operands that are not a mode (755, u+x, -w).
func chmodTargets(r []string) []string {
	var out []string
	for _, t := range r {
		if !chmodMode.MatchString(t) && !strings.HasPrefix(t, "-") {
			out = append(out, t)
		}
	}
	return out
}

// ownerTargets are the operands after chown's owner or chgrp's group (all of them with --reference).
func ownerTargets(r []string) []string {
	ops := operands(r)
	if hasGNUFlag(r, nil, "--reference") || len(ops) == 0 {
		return ops
	}
	return ops[1:]
}

func ddOutput(r []string) []string {
	var out []string
	for _, t := range r {
		if file, ok := strings.CutPrefix(t, "of="); ok {
			out = append(out, file)
		}
	}
	return out
}

// sedTargets are the files sed -i edits: the operands after the script.
func sedTargets(r []string) []string {
	if !sedInPlace(r) {
		return nil
	}
	scripts, ops := splitScripts(r, 'e', "--expression", "l", []string{"--line-length"})
	if len(scripts) == 0 && len(ops) > 0 {
		return ops[1:]
	}
	return ops
}

// yqTargets are the files yq -i rewrites: the operands after the expression.
func yqTargets(r []string) []string {
	f := readProgramFlags["yq"]
	if !shortFlag(r, "i", f.valued) && !hasGNUFlag(r, nil, "--inplace", "--in-place") {
		return nil
	}
	ops := operands(r, "-o", "--output-format", "-p", "--input-format", "-I", "--indent", "-s", "--split-exp")
	if len(ops) > 1 && !hasGNUFlag(r, nil, "--from-file") {
		return ops[1:]
	}
	return ops
}

func sortOutput(r []string) []string {
	return []string{shortValue(r, 'o', readProgramFlags["sort"].valued), gnuFlagValue(r, "--output")}
}

func curlOutputs(r []string) []string {
	return []string{shortValue(r, 'o', curlValuedShort), gnuFlagValue(r, "--output"), gnuFlagValue(r, "--output-dir"),
		shortValue(r, 'c', curlValuedShort), gnuFlagValue(r, "--cookie-jar"), shortValue(r, 'D', curlValuedShort),
		gnuFlagValue(r, "--dump-header"), gnuFlagValue(r, "--trace"), gnuFlagValue(r, "--trace-ascii"),
		gnuFlagValue(r, "--stderr"), gnuFlagValue(r, "--libcurl"), gnuFlagValue(r, "--etag-save")}
}

func wgetOutputs(r []string) []string {
	return []string{shortValue(r, 'O', wgetValuedShort), gnuFlagValue(r, "--output-document"),
		shortValue(r, 'o', wgetValuedShort), gnuFlagValue(r, "--output-file"), shortValue(r, 'a', wgetValuedShort),
		gnuFlagValue(r, "--append-output"), gnuFlagValue(r, "--save-cookies")}
}
