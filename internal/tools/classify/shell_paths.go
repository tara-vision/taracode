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
		words := expandWords(simple[assignmentsEnd(simple):])
		written := writtenOperands(words)
		for _, r := range seg.Redirects {
			if writesFile(r) {
				_, target, _ := strings.Cut(r, " ")
				written = append(written, target)
			}
		}
		files = files.with(lineFiles{written: underDirs(written, dirs), named: underDirs(namedOperands(words), dirs),
			landings: underDirs(landingOperands(words), dirs), finds: findTargets(words, dirs)})
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
func writtenOperands(words []string) []string { return operandsOf(words, writtenBy) }

// namedOperands returns the operands a command acts on by name only.
func namedOperands(words []string) []string { return operandsOf(words, namedBy) }

// landingOperands returns where a copy, move or link puts each source if its destination is a directory.
func landingOperands(words []string) []string { return operandsOf(words, landedBy) }

// operandFunc gives the operands of one program.
type operandFunc = func(rest []string) []string

// writtenBy, namedBy and landedBy look a program up in fileWriters, namedWriters and landingWriters. find's own
// functions stay out of the tables, since the commands find runs are read with them.
func writtenBy(prog string) (operandFunc, bool) {
	if prog == "find" {
		return findWritten, true
	}
	f, ok := fileWriters[prog]
	return f, ok
}

func namedBy(prog string) (operandFunc, bool) {
	if prog == "find" {
		return findNamed, true
	}
	f, ok := namedWriters[prog]
	return f, ok
}

func landedBy(prog string) (operandFunc, bool) {
	f, ok := landingWriters[prog]
	return f, ok
}

// operandsOf returns what a lookup gives for a command's operands, past the wrapper programs in front of it and
// read from the directory they move it to (env -C, sudo -D).
func operandsOf(words []string, lookup func(prog string) (operandFunc, bool)) []string {
	cmd, dir := behindWrappers(words, func(prog string) bool { _, ok := lookup(prog); return ok })
	if cmd == nil {
		return nil
	}
	f, _ := lookup(path.Base(cmd[0]))
	return inDir(dir, nonEmpty(f(cmd[1:])))
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
	"tar": tarWritten, "unzip": unzipWritten,
	"chmod": chmodTargets,
	"chown": ownerTargets, "chgrp": ownerTargets, "dd": ddOutput, "sed": sedTargets, "yq": yqTargets, "sort": sortOutput,
	"curl": curlOutputs, "wget": wgetOutputs,
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

// braceWords are the words the shell's brace expansion makes of a word ({a,b}, {1..3}): the word itself when it
// has none. Past braceLimit words, or for a sequence the classifier cannot follow, each brace expansion reads as
// a * instead, so the caller's glob and name checks still meet whatever it names.
func braceWords(word string) []string {
	if _, _, _, _, ok := braceSplit(word, braceSequence); !ok {
		return []string{word}
	}
	var out []string
	queue := []string{word}
	for n := 0; len(queue) > 0; n++ {
		w := queue[0]
		queue = queue[1:]
		prefix, alternatives, suffix, opaque, ok := braceSplit(w, braceSequence)
		switch {
		case n > braceLimit || opaque:
			return []string{braceGlob(word)}
		case !ok:
			out = append(out, w)
			continue
		}
		for _, a := range alternatives {
			queue = append(queue, prefix+a+suffix)
		}
	}
	return out
}

// braceGlob reads every brace expansion of a word as a *.
func braceGlob(word string) string {
	for {
		prefix, _, suffix, _, ok := braceSplit(word, braceSequence)
		if !ok {
			return word
		}
		word = prefix + "*" + suffix
	}
}

// expandWords brace-expands each word (braceWords).
func expandWords(words []string) []string {
	var out []string
	for _, w := range words {
		out = append(out, braceWords(w)...)
	}
	return out
}

// opaqueWords are the words of a line the classifier cannot see into, brace-expanded and without its options:
// any of them may name a file the line acts on.
func opaqueWords(segments []shellwords.Segment) []string {
	var out []string
	for _, seg := range segments {
		for _, w := range expandWords(seg.Words) {
			if !strings.HasPrefix(w, "-") {
				out = append(out, w)
			}
		}
	}
	return out
}

// dotGlobPossible reports a line that may turn on matching dot files before its globs expand, told from its
// parsed words: a word that names dotglob, globdots, globignore or bashopts in any case and spelling with
// underscores, a shopt or setopt, a set with an option bash's set does not have (zsh's set -4) or one named at
// run time, a variable named at run time (dynamicName), or code run in the shell itself that the classifier
// cannot see into: eval, source, a function the line defines, a shell run on a string (runsShellCode). An
// external program, opaque or not, cannot change the shell that expands the globs.
func dotGlobPossible(parsed shellwords.Result) bool {
	if parsed.FunctionDef || runsShellCode(parsed.Segments) {
		return true
	}
	for _, seg := range parsed.Segments {
		for _, w := range seg.Words {
			folded := strings.ToLower(strings.ReplaceAll(w, "_", ""))
			for _, name := range []string{"dotglob", "globdots", "globignore", "bashopts"} {
				if strings.Contains(folded, name) {
					return true
				}
			}
		}
		simple, _ := simpleCommand(seg.Words)
		words := unwrap(simple[assignmentsEnd(simple):]).words
		if dynamicName(words) || len(words) > 0 && in(path.Base(words[0]), "shopt",
			"setopt") || len(words) > 0 && words[0] == "set" && setChangesGlobs(words[1:]) {
			return true
		}
	}
	return false
}

// runsShellCode reports a segment that runs shell code the classifier cannot see through: eval, source or ., su
// or watch (which run their command through a shell), and a shell, also behind a wrapper such as xargs.
func runsShellCode(segments []shellwords.Segment) bool {
	for _, seg := range segments {
		simple, _ := simpleCommand(seg.Words)
		w := unwrap(simple[assignmentsEnd(simple):])
		if slices.Contains(w.wrappers, "watch") || len(w.words) > 0 && (shellNames[path.Base(w.words[0])] ||
			in(w.words[0], "eval", "source", ".", "su")) {
			return true
		}
	}
	return false
}

// setLetters are the short options of bash's set and o, which takes an option's name.
const setLetters = "abefhkmnptuvxBCEHPTo"

// setChangesGlobs reports a set whose options may change globbing: a letter bash's set does not have, or a
// word built at run time.
func setChangesGlobs(rest []string) bool {
	for _, w := range rest {
		if strings.ContainsAny(w, "$`") || len(w) > 1 && w != "--" && (w[0] == '-' || w[0] == '+') &&
			strings.Trim(w[1:], setLetters) != "" {
			return true
		}
	}
	return false
}

// dynamicName reports a variable a command names at run time, which may be GLOBIGNORE: the names export,
// declare, typeset, local, readonly and read set (any expansion in them, and in a declare -n's value), printf
// -v's and wait -p's. An assignment whose name holds an expansion, and let, getopts, mapfile and readarray,
// already make the line opaque.
func dynamicName(words []string) bool {
	dynamic := func(w string) bool { return strings.ContainsAny(w, "$`") }
	if len(words) == 0 {
		return false
	}
	switch prog, rest := path.Base(words[0]), words[1:]; {
	case in(prog, "export", "declare", "typeset", "local", "readonly", "read"):
		nameref := slices.ContainsFunc(rest, func(w string) bool { return looksLikeFlag(w) && strings.Contains(w, "n") })
		for _, w := range operands(rest) {
			if name, _, _ := strings.Cut(w, "="); dynamic(name) || nameref && dynamic(w) {
				return true
			}
		}
	case prog == "printf":
		return dynamic(flagValue(rest, "-v"))
	case prog == "wait":
		return dynamic(flagValue(rest, "-p"))
	}
	return false
}
