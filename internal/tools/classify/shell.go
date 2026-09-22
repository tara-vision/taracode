package classify

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// ShellResult is a shell classification plus the hosts the command line names.
type ShellResult struct {
	Result
	Hosts []string
}

var hostToken = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*\.[a-zA-Z]{2,}$`)

// Shell classifies a whole command line: every segment of every pipeline must read, no segment may
// redirect into a file, run in the background or use command substitution.
func Shell(command string) ShellResult {
	parsed, err := shellwords.Split(command)
	if err != nil {
		return ShellResult{Result: mutate("", "the command could not be parsed ("+err.Error()+")")}
	}
	if parsed.Substitution {
		return ShellResult{Result: mutate("", "command substitution ($(...) or backticks) hides what runs")}
	}
	out := ShellResult{Result: read("")}
	for _, seg := range parsed.Segments {
		words := stripAssignments(seg.Words)
		if len(words) == 0 {
			continue
		}
		if seg.Background {
			return ShellResult{Result: mutate(words[0], "background jobs (&) outlive the command timeout")}
		}
		for _, r := range seg.Redirects {
			if writesFile(r) {
				return ShellResult{Result: mutate(words[0], "the redirect "+r+" writes a file")}
			}
		}
		res := shellProgram(words)
		if res.Classification != policy.Read {
			return ShellResult{Result: res}
		}
		if out.Verb == "" {
			out.Verb = res.Verb
		}
		out.Hosts = append(out.Hosts, hostsIn(words)...)
	}
	return out
}

// writesFile reports whether a redirection creates or appends to a file (>, >>, &>, N>) rather than
// duplicating a descriptor (2>&1) or reading (<), and treats /dev/null and the standard streams
// as harmless targets.
func writesFile(redirect string) bool {
	op, target, _ := strings.Cut(redirect, " ")
	if !strings.Contains(op, ">") || strings.Contains(op, "&") && target == "" {
		return false
	}
	return !strings.HasPrefix(target, "/dev/")
}

// stripAssignments drops leading NAME=value words.
func stripAssignments(words []string) []string {
	for len(words) > 0 && strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") &&
		regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`).MatchString(words[0]) {
		words = words[1:]
	}
	return words
}

// shellWrapper classifies the programs that run another command in a modified context: sudo, su and
// doas escalate privileges outright; command, exec, nohup, time, nice, builtin, timeout and env run
// whatever follows them, so the wrapped command is classified by recursing into shellProgram. ok is
// false when prog is not one of these wrapper programs, so shellProgram falls through to its own
// switch.
func shellWrapper(prog string, rest []string) (Result, bool) {
	switch prog {
	case "sudo", "su", "doas":
		return mutate(prog, prog+" escalates privileges"), true
	case "command", "exec", "nohup", "time", "nice", "builtin":
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
		rest = stripAssignments(rest)
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
		if hasFlag(rest, "-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprintf", "-fls") {
			return mutate(prog, "find with -delete or -exec acts on the files it finds"), true
		}
		return read(prog), true
	case "sed":
		return sedResult(prog, rest), true
	case "awk", "gawk", "mawk", "nawk":
		if program := awkProgram(rest); strings.Contains(program, ">") || strings.Contains(program, "system(") {
			return mutate(prog, "the awk program writes files or runs commands"), true
		}
		return read(prog), true
	case "curl":
		return curlResult(rest), true
	case "wget":
		if hasFlag(rest, "-O-", "-qO-") || flagValue(rest, "-O", "--output-document") == "-" {
			return read(prog), true
		}
		return mutate(prog, "wget writes the download to a file (use curl or wget -O-)"), true
	case "make":
		if hasFlag(rest, "-n", "--dry-run", "--just-print", "-q", "--question", "-p", "--print-data-base") {
			return read(prog), true
		}
		return mutate(prog, "make runs build recipes"), true
	}
	return Result{}, false
}

func sedResult(prog string, rest []string) Result {
	for _, t := range rest {
		if sedInPlaceFlag(t) {
			return mutate(prog, "sed -i edits files in place")
		}
	}
	return read(prog)
}

func sedInPlaceFlag(t string) bool {
	if t == "-i" || t == "--in-place" || strings.HasPrefix(t, "--in-place=") {
		return true
	}
	return strings.HasPrefix(t, "-i") && !strings.HasPrefix(t, "-in")
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
	prog := filepath.Base(words[0])
	rest := words[1:]
	if res, ok := shellWrapper(prog, rest); ok {
		return res
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
			return read(prog + " " + rest[0])
		}
		return mutate(prog+" "+first(rest), prog+" "+first(rest)+" is not a read-only operation")
	}
	if readOnlyPrograms[prog] {
		return read(prog)
	}
	return mutate(prog, prog+" is not in the read-only allowlist")
}

func curlResult(rest []string) Result {
	if hasFlag(rest, "-o", "-O", "--output", "--remote-name", "--output-dir", "-T", "--upload-file", "-d", "--data",
		"--data-raw", "--data-binary", "--data-urlencode", "-F", "--form", "--json") {
		return mutate("curl", "curl with an output file or a request body is not a plain GET")
	}
	if method := strings.ToUpper(flagValue(rest, "-X", "--request")); method != "" && method != "GET" && method != "HEAD" {
		return mutate("curl", "curl -X "+method+" is not a read")
	}
	return read("curl")
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
	if in(op, "list", "view", "status", "checks", "diff", "download", "log") || group == "search" {
		return read("gh " + group + " " + op)
	}
	return mutate("gh "+group+" "+op, "gh "+group+" "+op+" changes GitHub state")
}

func awkProgram(rest []string) string {
	skip := false
	for _, t := range rest {
		if skip {
			skip = false
			continue
		}
		if in(t, "-F", "-v", "-f") {
			skip = true
			continue
		}
		if strings.HasPrefix(t, "-") {
			continue
		}
		return t
	}
	return ""
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

// hostsIn returns the host names on a command line: URL hosts and bare dotted names.
func hostsIn(words []string) []string {
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
