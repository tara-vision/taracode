package classify

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// ShellResult is a shell classification, the hosts the command line names, the files it writes or
// removes and the clusters its kubectl and helm mutations act on, as they are written on the line
// (the caller resolves them against the working directory and the kubeconfig).
type ShellResult struct {
	Result
	Hosts []string
	Paths []string
	Kube  []KubeTarget
}

var hostToken = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*\.[a-zA-Z]{2,}$`)

// Shell classifies a whole command line: every segment of every pipeline must read, no segment may
// redirect into a file, run in the background or use command substitution. Paths holds the files
// every segment writes or removes and Kube the clusters its kubectl and helm mutations act on,
// whatever the classification, so the protected targets apply.
func Shell(command string) ShellResult {
	parsed, err := shellwords.Split(command)
	if err != nil {
		return ShellResult{Result: mutate("", "the command could not be parsed ("+err.Error()+")")}
	}
	out := ShellResult{Result: read(""), Paths: shellPaths(parsed.Segments), Kube: shellKube(parsed)}
	if parsed.FunctionDef {
		out.Result = mutate("", "the line defines a shell function, whose body runs on the call and can shadow "+
			"any read-only name")
		return out
	}
	if parsed.Substitution {
		out.Result = mutate("", "command substitution ($(...), backticks, <(...), >(...)) or a translated $\"...\" "+
			"hides what runs")
		return out
	}
	var hosts []string
	vars := lineVars{}
	for _, seg := range parsed.Segments {
		res, words := shellSegment(seg, vars)
		if res.Classification != policy.Read {
			out.Result = res
			return out
		}
		if out.Verb == "" {
			out.Verb = res.Verb
		}
		if len(words) > 0 { // a segment of safe assignments only names no program and no host
			hosts = append(hosts, hostsIn(words)...)
		}
		vars.note(seg.Words)
	}
	out.Hosts = hosts
	return out
}

// shellSegment classifies one simple command and returns it without the shell's reserved words
// before it (do, then, !, {, ...) and without its assignment prefix. A redirect into a file, a
// background job, an assignment outside the safe list, an argument that expands a value the line
// controls (vars) or a program that is not a read makes it a mutation; a segment that only opens or
// closes a compound command, or is the head of a for loop or a case, runs nothing and reads.
func shellSegment(seg shellwords.Segment, vars lineVars) (Result, []string) {
	simple, header := simpleCommand(seg.Words)
	command := simple[assignmentsEnd(simple):]
	// Redirects and the background flag are checked before the empty-words case below: a segment
	// that is only a redirect ("> out.txt") or only an assignment ("NAME=value &") must never pass
	// just because it has no program to classify.
	for _, r := range seg.Redirects {
		if writesFile(r) {
			return mutate(first(command), "the redirect "+r+" writes a file"), nil
		}
	}
	if seg.Background {
		return mutate(first(command), "background jobs (&) outlive the command timeout"), nil
	}
	if header {
		if res, found := vars.expansionCheck(seg.Words, seg.Redirects, nil); found {
			return res, nil // for x in ${y:=-z}; the list's expansions assign too
		}
		return read(""), nil
	}
	words, res, ok := splitAssignments(simple)
	if !ok {
		return res, nil
	}
	if len(words) == 0 {
		return read(""), nil
	}
	if res, found := vars.expansionCheck(simple, seg.Redirects, words); found {
		return res, nil
	}
	return shellProgram(words), words
}

// harmlessTargets are the only files a redirect may name in a read: the null device and the
// standard streams. Every other path, /dev/sda and bash's /dev/tcp/host/port included, is a write.
var harmlessTargets = map[string]bool{
	"/dev/null": true, "/dev/stdout": true, "/dev/stderr": true, "/dev/tty": true, "/dev/fd/1": true, "/dev/fd/2": true,
}

// writesFile reports whether a redirection creates or appends to a file (>, >>, &>, >& file, N>)
// rather than duplicating a descriptor (2>&1) or reading (<).
func writesFile(redirect string) bool {
	op, target, _ := strings.Cut(redirect, " ")
	if !strings.Contains(op, ">") || strings.Contains(op, "&") && target == "" {
		return false
	}
	return !harmlessTargets[target]
}

var assignmentWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// safeAssignments lists the variables that are reads even by name shape (KUBECONFIG, AWS_PROFILE):
// splitAssignments checks this list before programVar, so a read may set them in front of its
// program. They pick a config file, a profile, a locale or an output style, never which program
// runs or what it loads (PATH, LD_PRELOAD and the like do both).
var safeAssignments = map[string]bool{
	"KUBECONFIG": true, "AWS_PROFILE": true, "AWS_REGION": true, "TZ": true, "LANG": true, "NO_COLOR": true,
	"PAGER": true,
}

func safeAssignment(name string) bool { return safeAssignments[name] || strings.HasPrefix(name, "LC_") }

// programVars change which program a name runs, what a program loads or how the shell reads the
// line; setting one is a mutation whatever the value. Any other variable set to a literal is a
// read: lineVars tracks where the line expands it, so a value that would add an option to a
// command is still refused at the command that uses it.
var programVars = map[string]bool{
	"PATH": true, "IFS": true, "ENV": true, "BASH_ENV": true, "CDPATH": true, "SHELLOPTS": true, "BASHOPTS": true,
	"PS4": true, "PROMPT_COMMAND": true, "HOME": true, "TMPDIR": true, "EDITOR": true, "VISUAL": true, "SHELL": true,
	"LESSOPEN": true, "LESSCLOSE": true, "GREP_OPTIONS": true, "MAKEFLAGS": true, "AWKPATH": true, "AWKLIBPATH": true,
	"PYTHONSTARTUP": true, "PERL5LIB": true, "PERL5OPT": true, "RUBYOPT": true, "RUBYLIB": true, "GOFLAGS": true,
	"GOROOT": true, "GOPROXY": true, "GOTOOLCHAIN": true, "GOOGLE_APPLICATION_CREDENTIALS": true,
}

// programVarPrefixes and programVarSuffixes catch the same class by name shape.
var (
	programVarPrefixes = []string{"LD_", "DYLD_", "GIT_", "KUBECTL_", "HELM_", "TF_", "DOCKER_", "CLOUDSDK_",
		"SSH_", "BASH_"}
	programVarSuffixes = []string{"PATH", "_HOME", "_DIR", "_FILE", "_CONFIG", "_OPTS", "_OPTIONS", "_ENV", "_PRELOAD",
		"_CMD", "_COMMAND", "_PROGRAM", "_EDITOR", "_PAGER", "_SHELL", "_BIN", "_EXEC"}
)

func programVar(name string) bool {
	if programVars[name] || hasPrefixIn(name, programVarPrefixes...) {
		return true
	}
	for _, s := range programVarSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// assignmentsEnd is the index of the first word that is not a leading NAME=value assignment.
func assignmentsEnd(words []string) int {
	i := 0
	for i < len(words) && assignmentWord.MatchString(words[i]) {
		i++
	}
	return i
}

// splitAssignments drops the leading NAME=value words. ok is false, with the mutate result, when a
// variable can change which program runs or what it loads (programVar); the safe list is checked
// first because KUBECONFIG ends in CONFIG.
func splitAssignments(words []string) (rest []string, res Result, ok bool) {
	end := assignmentsEnd(words)
	for _, w := range words[:end] {
		name, _, _ := strings.Cut(w, "=")
		if safeAssignment(name) {
			continue
		}
		if programVar(name) {
			return nil, mutate(name, "setting "+name+" can change which program runs or what it loads"), false
		}
	}
	return words[end:], Result{}, true
}

// systemBinDirs are the directories a path-qualified program word may name and still be classified
// by its name: they are root-owned, so /bin/cat is the cat the allowlist means.
var systemBinDirs = map[string]bool{"/bin": true, "/usr/bin": true, "/sbin": true, "/usr/sbin": true}

// programName returns the program a command word runs. A word with a slash names a file: ./cat is
// whatever that file is, so only a program in systemBinDirs keeps its name (ok is false otherwise).
func programName(word string) (string, bool) {
	if !strings.Contains(word, "/") {
		return word, true
	}
	clean := path.Clean(word)
	if strings.HasPrefix(clean, "/") && systemBinDirs[path.Dir(clean)] {
		return path.Base(clean), true
	}
	return word, false
}

// shellWrapper classifies the programs that run another command in a modified context: sudo, su and
// doas escalate privileges outright; command, exec, nohup, time, nice, builtin, timeout and env run
// whatever follows them, so the wrapped command is classified by recursing into shellProgram (command
// -v and -V are the exception: they describe a command without running it). ok is false when prog is
// not one of these wrapper programs, so shellProgram falls through to its own switch.
func shellWrapper(prog string, rest []string) (Result, bool) {
	switch prog {
	case "sudo", "su", "doas":
		return mutate(prog, prog+" escalates privileges"), true
	case "command":
		if in(first(rest), "-v", "-V") {
			return read(prog), true // describes a command; runs nothing
		}
		if first(rest) == "-p" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return read(prog), true
		}
		return shellProgram(rest), true
	case "exec", "nohup", "time", "nice", "builtin":
		if len(rest) == 0 {
			return read(prog), true
		}
		return shellProgram(rest), true
	case "timeout":
		if len(rest) < 2 {
			return read(prog), true
		}
		return shellProgram(rest[1:]), true
	case "env":
		rest, res, ok := splitAssignments(rest)
		if !ok {
			return res, true
		}
		if len(rest) == 0 {
			return read(prog), true
		}
		return shellProgram(rest), true
	}
	return Result{}, false
}

// shellDevopsTool classifies the infrastructure CLIs by delegating to their dedicated classifiers.
// ok is false when prog is none of them.
func shellDevopsTool(prog string, rest []string) (Result, bool) {
	switch prog {
	case "git":
		return Git(rest), true
	case "kubectl":
		return Kubectl(first(rest), tail(rest)), true
	case "helm":
		return Helm(rest), true
	case "terraform", "tofu":
		return Terraform(first(stripChdir(rest)), tail(stripChdir(rest))), true
	case "docker", "podman", "nerdctl":
		return Docker(rest), true
	case "aws", "az", "gcloud":
		return Cloud(prog, rest), true
	}
	return Result{}, false
}

// shellFileTool classifies the programs whose read or mutate form depends on their flags or their
// script argument: find, sed, awk and its variants, curl, wget and make. ok is false when prog is
// none of them.
func shellFileTool(prog string, rest []string) (Result, bool) {
	switch prog {
	case "find":
		if hasFlag(rest, "-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprint0", "-fprintf", "-fls") {
			return mutate(prog, "find with -delete, -exec or -fprint acts on the files it finds or writes one"), true
		}
		return read(prog), true
	case "sed":
		return sedResult(prog, rest), true
	case "awk", "gawk", "mawk", "nawk":
		return awkResult(prog, rest), true
	case "curl":
		return curlResult(rest), true
	case "wget":
		return wgetResult(rest), true
	case "make":
		if hasFlag(rest, "-n", "--dry-run", "--just-print", "-q", "--question", "-p", "--print-data-base") {
			return read(prog), true
		}
		return mutate(prog, "make runs build recipes"), true
	}
	return Result{}, false
}

// shellMiscTool classifies gh, the scripting language runtimes, sysctl and the programs that always
// run or write whatever follows them. ok is false when prog is none of them.
func shellMiscTool(prog string, rest []string) (Result, bool) {
	switch prog {
	case "gh":
		return ghResult(rest), true
	case "python", "python2", "python3", "node", "ruby", "perl", "php", "java", "deno", "bun":
		if len(rest) == 1 && in(rest[0], "--version", "-V", "-v", "-version", "version") {
			return read(prog), true
		}
		return mutate(prog, prog+" runs arbitrary code"), true
	case "sysctl":
		if hasFlag(rest, "-w", "--write") || strings.Contains(strings.Join(rest, " "), "=") {
			return mutate(prog, "sysctl -w changes kernel parameters"), true
		}
		return read(prog), true
	case "xargs", "tee", "parallel":
		return mutate(prog, prog+" runs or writes whatever follows it"), true
	}
	return Result{}, false
}

// shellProgram classifies one simple command by its program name.
func shellProgram(words []string) Result {
	prog, ok := programName(words[0])
	if !ok {
		return mutate(prog, prog+" names a program file outside /bin, /usr/bin, /sbin and /usr/sbin, "+
			"so it runs whatever that file is")
	}
	rest := words[1:]
	if res, ok := shellWrapper(prog, rest); ok {
		return res
	}
	if args, ok := distroKubectl(prog, rest); ok {
		return Kubectl(first(args), tail(args)) // microk8s/k3s/minikube kubectl <verb>
	}
	if res, ok := shellDevopsTool(prog, rest); ok {
		return res
	}
	if res, ok := shellFileTool(prog, rest); ok {
		return res
	}
	if res, ok := shellMiscTool(prog, rest); ok {
		return res
	}
	if reads, ok := subcommandReads[prog]; ok {
		if len(rest) > 0 && in(rest[0], reads...) {
			if res, writes := subcommandWrites(prog, rest); writes {
				return res
			}
			return read(prog + " " + rest[0])
		}
		return mutate(prog+" "+first(rest), prog+" "+first(rest)+" is not a read-only operation")
	}
	if readOnlyPrograms[prog] {
		if res, writes := readProgramWrites(prog, rest); writes {
			return res
		}
		return read(prog)
	}
	return mutate(prog, prog+" is not in the read-only allowlist")
}

func ghResult(rest []string) Result {
	if len(rest) < 2 {
		return read("gh")
	}
	group, op := rest[0], rest[1]
	if group == "api" {
		if hasFlag(rest, "-X", "--method", "-f", "-F", "--field", "--raw-field", "--input") {
			return mutate("gh api", "gh api with a method or fields changes GitHub state")
		}
		return read("gh api")
	}
	// download writes the run artifacts or release assets into the working directory.
	if in(op, "list", "view", "status", "checks", "diff", "log") || group == "search" {
		return read("gh " + group + " " + op)
	}
	return mutate("gh "+group+" "+op, "gh "+group+" "+op+" changes GitHub state or writes files")
}

func stripChdir(tokens []string) []string {
	for len(tokens) > 0 && strings.HasPrefix(tokens[0], "-chdir=") {
		tokens = tokens[1:]
	}
	return tokens
}

func tail(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	return tokens[1:]
}

// nonHostSuffixes are file extensions that make a dotted, host-shaped token a filename rather than a
// host name.
var nonHostSuffixes = []string{".log", ".txt", ".json", ".yaml", ".yml", ".tf", ".go"}

// hostsIn returns the host names in a command's arguments: URL hosts and bare dotted names.
func hostsIn(words []string) []string {
	if len(words) < 2 {
		return nil
	}
	var hosts []string
	for _, w := range words[1:] {
		if u, err := url.Parse(w); err == nil && u.Host != "" {
			hosts = append(hosts, u.Hostname())
			continue
		}
		if hostToken.MatchString(w) && !hasSuffixIn(w, nonHostSuffixes...) {
			hosts = append(hosts, w)
		}
	}
	return hosts
}

func hasSuffixIn(value string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(value, s) {
			return true
		}
	}
	return false
}
