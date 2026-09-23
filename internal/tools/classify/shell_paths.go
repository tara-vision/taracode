package classify

import (
	"path"
	"regexp"
	"strings"

	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// shellPaths returns the files the segments write or remove, as written on the line: the targets of
// redirects into files and the operands fileWriters names, also behind sudo, env, xargs and the
// other wrappers and after the shell's reserved words (do, then, !, {, ...). A relative path is
// also returned joined to every literal directory an earlier cd or pushd of the line moved to, since
// the caller resolves it against the directory the line starts in. Paths computed at run time
// (variables, command substitution) are not seen.
func shellPaths(segments []shellwords.Segment) []string {
	var paths, dirs []string
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
		for _, p := range written {
			paths = append(paths, p)
			if relativePath(p) {
				for _, d := range dirs {
					paths = append(paths, path.Join(d, p))
				}
			}
		}
		if d, ok := cdTarget(words, dirs); ok {
			dirs = append(dirs, d)
		}
	}
	return paths
}

// relativePath reports a path the working directory resolves: not absolute, not under ~ and not
// starting with a variable.
func relativePath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~") && !strings.HasPrefix(p, "$")
}

// cdTarget returns the directory a cd or pushd with a literal argument moves to, joined to the
// previous one when it is relative. ok is false for any other command and for a directory known
// only at run time (cd -, cd $DIR).
func cdTarget(words, dirs []string) (string, bool) {
	if len(words) == 0 || len(words) > 2 || !in(words[0], "cd", "pushd") {
		return "", false
	}
	dir := "~"
	if len(words) == 2 {
		dir = words[1]
	}
	if dir == "-" || strings.ContainsAny(dir, "$`*?[") {
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
	"ionice": true, "watch": true,
}

// writtenOperands returns the operands a file-writing command writes or removes.
func writtenOperands(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	prog, rest := path.Base(words[0]), words[1:]
	if wrapperPrograms[prog] {
		for i, w := range rest {
			if _, ok := fileWriters[path.Base(w)]; ok {
				return writtenOperands(rest[i:])
			}
		}
		return nil
	}
	if writer, ok := fileWriters[prog]; ok {
		return nonEmpty(writer(rest))
	}
	return nil
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
	"mv": func(r []string) []string {
		return append(operands(r, "-t", "--target-directory", "-S", "--suffix"), flagValue(r, "-t", "--target-directory"))
	},
	"cp": destination, "install": destination, "ln": linkName, "chmod": chmodTargets, "chown": ownerTargets,
	"chgrp": ownerTargets, "dd": ddOutput, "sed": sedTargets, "yq": yqTargets, "sort": sortOutput,
	"curl": curlOutputs, "wget": wgetOutputs, "find": findTargets,
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

// destination is where cp and install write: the -t directory, or the last operand.
func destination(r []string) []string {
	if dir := flagValue(r, "-t", "--target-directory"); dir != "" {
		return []string{dir}
	}
	ops := operands(r, "-t", "--target-directory", "-S", "--suffix", "-m", "--mode", "-o", "--owner", "-g", "--group")
	if len(ops) == 0 {
		return nil
	}
	return ops[len(ops)-1:]
}

// linkName is the link ln creates: the -t directory, the last operand, or the target's base name in
// the working directory when only the target is given.
func linkName(r []string) []string {
	if dir := flagValue(r, "-t", "--target-directory"); dir != "" {
		return []string{dir}
	}
	ops := operands(r, "-t", "--target-directory", "-S", "--suffix")
	switch len(ops) {
	case 0:
		return nil
	case 1:
		return []string{path.Base(ops[0])}
	}
	return ops[len(ops)-1:]
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

// findTargets are the files find writes (-fprint, -fls) and, when it deletes or runs a command on
// what it finds, its starting points and the name patterns it matches.
func findTargets(r []string) []string {
	out := []string{flagValue(r, "-fprint"), flagValue(r, "-fprint0"), flagValue(r, "-fprintf"), flagValue(r, "-fls")}
	if !hasFlag(r, "-delete", "-exec", "-execdir", "-ok", "-okdir") {
		return out
	}
	for _, t := range r {
		if strings.HasPrefix(t, "-") || t == "(" || t == "!" {
			break
		}
		out = append(out, t)
	}
	return append(out, flagValue(r, "-name"), flagValue(r, "-iname"), flagValue(r, "-path"), flagValue(r, "-ipath"),
		flagValue(r, "-wholename"))
}
