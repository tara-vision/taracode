package classify

import (
	"path"
	"strings"
)

// wrapperOptionValues are the options of the wrapper programs that take the next word as their value.
var wrapperOptionValues = map[string][]string{
	"sudo": {"-u", "-g", "-C", "-D", "-h", "-p", "-r", "-t", "-T", "-U", "--user", "--group", "--close-from",
		"--chdir", "--host", "--prompt", "--role", "--type", "--command-timeout", "--other-user"},
	"doas":    {"-u", "-C"},
	"env":     {"-u", "--unset", "-C", "--chdir", "-S", "--split-string"},
	"exec":    {"-a"},
	"nice":    {"-n", "--adjustment"},
	"timeout": {"-s", "--signal", "-k", "--kill-after"},
	"time":    {"-f", "--format", "-o", "--output"},
	"xargs": {"-I", "-n", "-L", "-P", "-s", "-E", "-d", "-a", "--max-args", "--max-lines", "--max-procs",
		"--max-chars", "--delimiter", "--arg-file", "--replace", "--eof"},
	"parallel": {"-j", "--jobs", "-S", "--sshlogin", "--delay", "--timeout", "-a", "--arg-file"},
	"watch":    {"-n", "--interval"},
	"ionice":   {"-c", "-n", "-p", "--class", "--classdata", "--pid"},
	"stdbuf":   {"-i", "-o", "-e", "--input", "--output", "--error"},
}

// wrapped is a command with the wrapper programs in front of it taken off.
type wrapped struct {
	words    []string // the program the wrappers run and its arguments; empty when there is none
	wrappers []string // the wrappers, outermost first
	options  []string // the wrappers' options, as "env -i"
	env      []string // the NAME=value words env or sudo set for the program
}

// unwrap takes the wrapper programs off a command (sudo, env, timeout, xargs, ...), with their
// options, the values those take, the NAME=value words env and sudo read and timeout's duration, so
// the program they run is in program position.
func unwrap(words []string) wrapped {
	var w wrapped
	for len(words) > 0 && wrapperPrograms[path.Base(words[0])] {
		prog := path.Base(words[0])
		w.wrappers = append(w.wrappers, prog)
		words = w.skipWrapperArgs(prog, words[1:])
	}
	w.words = words
	return w
}

// skipWrapperArgs returns the words after one wrapper's own arguments.
func (w *wrapped) skipWrapperArgs(prog string, words []string) []string {
	duration := prog == "timeout"
	for len(words) > 0 {
		t := words[0]
		switch {
		case t == "--":
			return words[1:]
		case looksLikeFlag(t):
			w.options = append(w.options, prog+" "+t)
			words = words[1:]
			if name, _, inline := strings.Cut(t, "="); !inline && in(name, wrapperOptionValues[prog]...) && len(words) > 0 {
				words = words[1:]
			}
		case (prog == "env" || prog == "sudo") && assignmentWord.MatchString(t):
			w.env = append(w.env, t)
			words = words[1:]
		case duration:
			duration = false
			words = words[1:]
		default:
			return words
		}
	}
	return words
}

// changesEnvironment reports wrappers that give the program another environment or user than the
// line's: sudo and doas (another user, its HOME and a reset environment), and env with an option
// (-i clears it, -u unsets a variable, -C and -S change the directory or split a string).
func (w wrapped) changesEnvironment() bool {
	for _, prog := range w.wrappers {
		if prog == "sudo" || prog == "doas" {
			return true
		}
	}
	for _, opt := range w.options {
		if strings.HasPrefix(opt, "env ") {
			return true
		}
	}
	return false
}

// feedsArguments reports xargs or parallel in front of the program: they add arguments read at run
// time, so any option, the namespace and the verb included, can change.
func (w wrapped) feedsArguments() bool {
	for _, prog := range w.wrappers {
		if prog == "xargs" || prog == "parallel" {
			return true
		}
	}
	return false
}
