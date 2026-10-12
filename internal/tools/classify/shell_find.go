package classify

import (
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/tara-vision/taracode/internal/policy"
)

// findNameTests are the tests that narrow what find acts on to the names or paths their patterns match. -lname
// and -ilname match a link's target, which the policy cannot walk for, so they narrow nothing.
var findNameTests = map[string]bool{"-name": true, "-iname": true, "-path": true, "-ipath": true, "-wholename": true,
	"-iwholename": true, "-regex": true, "-iregex": true}

// findTargets are the finds a segment runs that narrow by name what they delete or run a command on, or that
// follow symbolic links, for the policy to check: each start point, read from the directory wrappers move find to
// and from every directory an earlier cd of the line moved to, with the start point as written, the expression's
// name tests and whether it follows links.
func findTargets(words, dirs []string) []policy.Find {
	cmd, dir := behindWrappers(words, func(prog string) bool { return prog == "find" })
	if cmd == nil {
		return nil
	}
	opts, starts, expr := findStarts(cmd[1:])
	follows := findFollows(opts, expr)
	if !findActs(expr) || !follows && !findNarrows(expr) {
		return nil
	}
	tests := findTests(expr)
	var out []policy.Find
	for _, start := range starts {
		for _, s := range underDirs([]string{joinDir(dir, start)}, dirs) {
			out = append(out, policy.Find{Start: s, Written: start, Tests: tests, Follow: follows})
		}
	}
	return out
}

// findFollows reports a find that follows symbolic links under its start points: -L last among the -H, -L and
// -P options before them, or -follow anywhere in its expression.
func findFollows(opts, expr []string) bool {
	follows := false
	for _, o := range opts {
		switch o {
		case "-L":
			follows = true
		case "-H", "-P":
			follows = false
		}
	}
	return follows || slices.Contains(expr, "-follow")
}

// findWritten are the files find writes (-fprint, -fls) and, when it deletes or runs a command on what it finds
// and its expression does not narrow that by name (findNarrows), its start points: it acts on whatever they hold.
func findWritten(r []string) []string {
	out := []string{flagValue(r, "-fprint"), flagValue(r, "-fprint0"), flagValue(r, "-fprintf"), flagValue(r, "-fls")}
	_, starts, expr := findStarts(r)
	if findActs(expr) && !findNarrows(expr) {
		out = append(out, starts...)
	}
	return out
}

// findNamed are find's start points and the patterns of its name tests, when it deletes or runs a command on what
// it finds and its expression narrows that by name: it acts on what the patterns match, not on the start points.
func findNamed(r []string) []string {
	_, starts, expr := findStarts(r)
	if !findActs(expr) || !findNarrows(expr) {
		return nil
	}
	return slices.Concat(starts, findNames(expr))
}

// findNarrows reports a find expression narrowed by name (findNarrowed) whose commands act only on the entries
// it hands them (actsBeyondMatches).
func findNarrows(expr []string) bool {
	if !findNarrowed(expr) {
		return false
	}
	for _, c := range execCommands(expr) {
		if c.actsBeyondMatches() {
			return false
		}
	}
	return true
}

// findActions are the actions of find that run a command: its words up to a ; or, after {}, a +.
var findActions = map[string]bool{"-exec": true, "-execdir": true, "-ok": true, "-okdir": true}

// findCommand is a command find runs: the action that runs it and its words.
type findCommand struct {
	action string
	words  []string
}

// actsBeyondMatches reports a command find runs that may act on paths other than the entries it hands it: one
// that runs code (a shell, eval, su, an interpreter, make, a script or the match itself), a word that leads from
// {} to another path (rm -rf {}/..), and, for -execdir and -okdir, which run in each match's directory, a
// relative path among its targets.
func (c findCommand) actsBeyondMatches() bool {
	for _, w := range c.words {
		if strings.Contains(w, "{}") && w != "{}" && strings.Contains(w, "/") {
			return true
		}
	}
	w := unwrap(c.words)
	if len(w.words) > 0 {
		prog := path.Base(w.words[0])
		if shellNames[prog] || codeRunners[prog] || in(prog, "eval", "su") || strings.Contains(w.words[0], "/") ||
			w.words[0] == "{}" || w.feedsArguments() || slices.Contains(w.wrappers, "watch") {
			return true
		}
	}
	if c.action == "-execdir" || c.action == "-okdir" {
		files := execFiles(c.words, "", nil)
		return slices.ContainsFunc(slices.Concat(files.written, files.named, files.landings), relativePath)
	}
	return false
}

// execCommands are the commands a find expression runs with -exec, -execdir, -ok and -okdir.
func execCommands(expr []string) []findCommand {
	var out []findCommand
	var cmd findCommand
	running := false
	for _, w := range expr {
		switch {
		case !running && findActions[w]:
			running, cmd = true, findCommand{action: w}
		case running && (w == ";" || w == "+" && len(cmd.words) > 0 && cmd.words[len(cmd.words)-1] == "{}"):
			out, running = append(out, cmd), false
		case running:
			cmd.words = append(cmd.words, w)
		}
	}
	if running {
		out = append(out, cmd)
	}
	return out
}

// findExecs are the commands a segment's find runs, as their words, with the directory wrappers move find to,
// where they run.
func findExecs(words []string) ([][]string, string) {
	cmd, dir := behindWrappers(words, func(prog string) bool { return prog == "find" })
	if cmd == nil {
		return nil, ""
	}
	_, _, expr := findStarts(cmd[1:])
	var out [][]string
	for _, c := range execCommands(expr) {
		if len(c.words) > 0 {
			out = append(out, c.words)
		}
	}
	return out, dir
}

// segmentCommands are the commands a segment runs: its own words and the commands its find runs.
func segmentCommands(words []string) [][]string {
	execs, _ := findExecs(words)
	return append([][]string{words}, execs...)
}

// execFiles are the files a command that find runs acts on, read from find's directory, without the entries find
// hands it ({}), which the find's own check covers.
func execFiles(cmd []string, chdir string, dirs []string) lineFiles {
	keep := func(paths []string) []string {
		var out []string
		for _, p := range inDir(chdir, paths) {
			if !strings.Contains(p, "{}") {
				out = append(out, p)
			}
		}
		return underDirs(out, dirs)
	}
	return lineFiles{written: keep(writtenOperands(cmd)), named: keep(namedOperands(cmd)),
		landings: keep(landingOperands(cmd))}
}

// findActs reports an expression that deletes or runs a command on what find finds.
func findActs(expr []string) bool {
	return hasFlag(expr, "-delete", "-exec", "-execdir", "-ok", "-okdir")
}

// findStarts splits find's arguments into the options before its start points (-H, -L, -P, -O level and -D
// with its debug options), its start points and its expression.
func findStarts(r []string) (opts, starts, expr []string) {
	i := 0
	for i < len(r) && (in(r[i], "-H", "-L", "-P", "-D") || strings.HasPrefix(r[i], "-O")) {
		if r[i] == "-D" && i+1 < len(r) {
			i++
		}
		i++
	}
	j := i
	for j < len(r) && !strings.HasPrefix(r[j], "-") && !in(r[j], "(", ")", "!", ",") {
		j++
	}
	return r[:i], r[i:j], r[j:]
}

// findNarrowed reports whether every branch of a find expression (split at -o, -or and the comma outside
// parentheses) narrows what find acts on by name (branchNarrowed).
func findNarrowed(expr []string) bool {
	depth, start := 0, 0
	for i, t := range expr {
		switch {
		case t == "(":
			depth++
		case t == ")":
			depth--
		case depth == 0 && in(t, "-o", "-or", ","):
			if !branchNarrowed(expr[start:i]) {
				return false
			}
			start = i + 1
		}
	}
	return branchNarrowed(expr[start:])
}

// branchNarrowed reports a branch of a find expression that holds a name test whose pattern has a letter or a
// digit, not negated, or a group in parentheses that is narrowed itself. A pattern of wildcards alone (*, .*)
// matches everything a start point holds, .git included.
func branchNarrowed(branch []string) bool {
	for i := 0; i < len(branch); i++ {
		switch t := branch[i]; {
		case t == "!" || t == "-not":
			i = primaryEnd(branch, i+1)
		case t == "(":
			end := closingParen(branch, i)
			if findNarrowed(branch[i+1 : end]) {
				return true
			}
			i = end
		case findNameTests[t] && i+1 < len(branch):
			if strings.IndexFunc(branch[i+1], func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
				return true
			}
			i++
		}
	}
	return false
}

// primaryEnd returns the index of the last word of the find primary that starts at i: a group in parentheses, a
// name test with its pattern, or one word.
func primaryEnd(branch []string, i int) int {
	switch {
	case i >= len(branch):
		return i
	case branch[i] == "(":
		return closingParen(branch, i)
	case findNameTests[branch[i]]:
		return i + 1
	}
	return i
}

// closingParen returns the index of the ) that closes the ( at i, or the end of the words when none does.
func closingParen(words []string, i int) int {
	depth := 0
	for j := i; j < len(words); j++ {
		switch words[j] {
		case "(":
			depth++
		case ")":
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return len(words)
}

// findTests are the name tests of a find expression.
func findTests(expr []string) []policy.NameTest {
	var out []policy.NameTest
	for i := 0; i+1 < len(expr); i++ {
		if findNameTests[expr[i]] {
			out = append(out, policy.NameTest{Test: expr[i], Pattern: expr[i+1]})
			i++
		}
	}
	return out
}

// findNames are the patterns of a find expression's name tests.
func findNames(expr []string) []string {
	var out []string
	for _, t := range findTests(expr) {
		out = append(out, t.Pattern)
	}
	return out
}
