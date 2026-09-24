package classify

import (
	"path"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// KubeTarget is the cluster a kubectl or helm mutation of a shell line acts on, as the line names
// it. An empty Context or Namespace is the current one of the kubeconfig (for helm, the
// HELM_KUBECONTEXT and HELM_NAMESPACE of taracode's environment first); "*" is every namespace (-A),
// several values, or one the classifier cannot know. Kubeconfig is the kubeconfig the command names:
// its --kubeconfig, else a KUBECONFIG its prefix or env sets.
type KubeTarget struct {
	Context, Namespace, Kubeconfig string
	Helm                           bool
	Cause                          string // why Context or Namespace is "*": one short phrase, "" when concrete
}

// The causes a kube target is "*": one short phrase per kind, carried into the deny reason with a
// remedy so a model does not retry the same line.
const (
	causeContextSwitch = "the context is switched earlier on the line"
	causeKubeconfigSet = "the kubeconfig is set by an assignment earlier on the line"
	causeRunTimeArg    = "an argument is computed at run time"
	causeWrapper       = "sudo, xargs or env -i runs kubectl"
	causeUnknownProg   = "a program before kubectl is not known"
	causeConflicting   = "conflicting --context, -n or --kubeconfig values"
	causeFunctionDef   = "kubectl runs inside a shell function"
	causeEnvVariable   = "a variable set for the command changes what kubectl reads"
	causeRelativeKube  = "a relative kubeconfig is read from a directory changed earlier"
)

// kubeLine collects the targets of a shell line's kubectl and helm mutations, segment by segment.
type kubeLine struct {
	targets     []KubeTarget
	named       []string // the kubeconfig files the line names anywhere
	vars        lineVars // the variables earlier segments set
	changed     bool     // an earlier segment may have changed the kube configuration
	changeCause string   // why (the earliest reason), carried into the deny
	moved       bool     // an earlier cd, pushd or popd: a relative kubeconfig is another file
	opaque      bool     // the line runs something the classifier cannot see into
	words       int      // the words of the line that name kubectl or helm
	placed      int      // of them, the ones read as the program of a command
}

// noteChanged records that an earlier segment may have changed the kube configuration, keeping the
// earliest cause so the deny names why the later command's context is "*".
func (l *kubeLine) noteChanged(cause string) {
	l.changed = true
	if l.changeCause == "" {
		l.changeCause = cause
	}
}

// shellKube returns the clusters the kubectl and helm mutations of a shell line act on (the rules
// are on segment and kubeCommand), and a target of "*" when a word names kubectl or helm where the
// classifier does not read it as a program (an argument of sh, eval, find -exec, docker run, kubectl
// exec, a program it does not know) on a line that runs something it cannot see into.
func shellKube(parsed shellwords.Result) []KubeTarget {
	if parsed.FunctionDef {
		// A function body runs on the call, with arguments the classifier cannot correlate, so a
		// kubectl or helm anywhere on the line acts on a context it cannot know.
		var words int
		for _, seg := range parsed.Segments {
			words += countKubeWords(seg.Words)
		}
		if words > 0 {
			return []KubeTarget{{Context: "*", Namespace: "*", Cause: causeFunctionDef}}
		}
		return nil
	}
	l := kubeLine{opaque: parsed.Substitution, named: namedKubeconfigs(parsed.Segments), vars: lineVars{}}
	for _, seg := range parsed.Segments {
		l.segment(seg)
	}
	if l.opaque && l.words > l.placed {
		l.targets = append(l.targets, KubeTarget{Context: "*", Namespace: "*", Cause: causeUnknownProg})
	}
	return l.targets
}

// segment reads one simple command after the shell's reserved words (for, do, then, !, {, ...),
// and notes whether it may change the kube configuration for the commands after it: it writes a
// kubeconfig, switches the context, sets a variable outside the safe ones, or runs a program the
// classifier does not know.
func (l *kubeLine) segment(seg shellwords.Segment) {
	l.words += countKubeWords(seg.Words)
	words, header := simpleCommand(seg.Words)
	words = withoutGluedBrace(words)
	if l.writesKubeconfig(words, seg.Redirects) {
		l.noteChanged(causeKubeconfigSet)
	}
	if !header {
		l.command(words)
	}
	l.vars.note(seg.Words)
}

// command reads a simple command; when it may change the kube configuration for the commands after
// it, it records that with noteChanged.
func (l *kubeLine) command(words []string) {
	end := assignmentsEnd(words)
	prefix, command := words[:end], words[end:]
	if len(command) == 0 {
		if !neutralNames(prefix) { // KUBECONFIG=x; or export's plain twin: sh may export it
			l.noteChanged(exportCause(prefix))
		}
		return
	}
	w := unwrap(command)
	if len(w.words) == 0 { // a wrapper alone; with an option it may run a string (env -S "kubectl ...")
		if len(w.options) > 0 {
			l.opaque = true
			l.noteChanged(causeUnknownProg)
		}
		return
	}
	prog := path.Base(w.words[0])
	if args, ok := distroKubectl(prog, w.words[1:]); ok {
		kw := w
		kw.words = append([]string{"kubectl"}, args...)
		l.placed++
		l.kubeCommand("kubectl", kw, append(append([]string{}, prefix...), w.env...))
		return
	}
	if prog != "kubectl" && prog != "helm" {
		l.otherCommand(prog, w)
		return
	}
	l.placed++
	l.kubeCommand(prog, w, append(append([]string{}, prefix...), w.env...))
	if prog == "kubectl" && kubectlChangesConfig(w.words[1:], l.named) {
		l.noteChanged(causeContextSwitch)
	}
}

// distroKubectl reports the kubectl arguments when prog is a single-binary Kubernetes distribution
// that runs kubectl as a subcommand (microk8s, k3s, k0s, minikube). The classifier reads the line by
// the kubectl verb after it, so a read stays a read; minikube separates the arguments with "--". ok
// is false when prog is not such a wrapper or its subcommand is not kubectl.
func distroKubectl(prog string, rest []string) ([]string, bool) {
	if !kubectlDistros[prog] || len(rest) == 0 || rest[0] != "kubectl" {
		return nil, false
	}
	args := rest[1:]
	if len(args) > 0 && args[0] == "--" { // minikube kubectl -- get pods
		args = args[1:]
	}
	return args, true
}

// kubectlDistros run kubectl as a subcommand: "<distro> kubectl <verb>" is classified by that verb.
var kubectlDistros = map[string]bool{"microk8s": true, "k3s": true, "k0s": true, "minikube": true}

// kubeCommand records the target of a kubectl or helm mutation, read from its flags and from env,
// the NAME=value words set for it (its prefix, then env's or sudo's). The context is "*", and so is
// a namespace the command does not name, when the configuration it reads is not the one the
// classifier sees: an earlier segment may have changed it, sudo, doas or env -i/-u run the command
// in another environment, a variable outside the safe ones is set for it, or its kubeconfig is
// relative after a cd. Behind xargs or parallel, and with an argument the shell computes (a
// variable other than a plain one the line set, a substitution), both are "*": the words added at
// run time can name any context or namespace, and they make a read a mutation when they can (a
// --dry-run=none among them wins; kubectl get stays a read whatever is added).
func (l *kubeLine) kubeCommand(prog string, w wrapped, env []string) {
	tokens := w.words[1:]
	t, mutates := kubeTarget(prog, tokens)
	if prog == "kubectl" && kubectlRunsCommands(tokens) {
		l.opaque = true
	}
	envUnknown := t.applyEnv(env)
	switch {
	case w.feedsArguments() || l.runTimeArguments(tokens):
		_, mutates = kubeTarget(prog, append(append([]string{}, tokens...), "--dry-run=none"))
		t.Context, t.Namespace = "*", "*"
		if w.feedsArguments() {
			t.Cause = causeWrapper
		} else {
			t.Cause = causeRunTimeArg
		}
	case envUnknown || l.changed || w.changesEnvironment() || l.moved && relativeKubeconfig(t.Kubeconfig):
		t.Context = "*"
		if t.Namespace == "" {
			t.Namespace = "*"
		}
		t.Cause = l.opaqueCause(w, envUnknown)
	default:
		if t.Context == "*" || t.Namespace == "*" {
			t.Cause = causeConflicting // two --context or -n values, or an ambiguous short cluster
		}
	}
	if mutates {
		l.targets = append(l.targets, t)
	}
}

// opaqueCause names why a kubectl or helm mutation's context is "*": the most specific reason among
// the wrapper, an earlier change on the line, a variable set for the command, and a relative
// kubeconfig read after a cd.
func (l *kubeLine) opaqueCause(w wrapped, envUnknown bool) string {
	switch {
	case w.changesEnvironment():
		return causeWrapper
	case l.changed && l.changeCause != "":
		return l.changeCause
	case envUnknown:
		return causeEnvVariable
	case l.moved:
		return causeRelativeKube
	default:
		return causeContextSwitch
	}
}

// runTimeArguments reports an argument before "--" whose value the shell computes, or whose brace
// expansion yields an option.
func (l *kubeLine) runTimeArguments(tokens []string) bool {
	for _, t := range beforeDoubleDash(tokens) {
		if l.vars.runTime(t) || braceOption(t) {
			return true
		}
	}
	return false
}

// runTime reports a word whose value the classifier cannot read: a ${...} with an operator, a
// substitution, a special or positional parameter ($@, $*, $1..$9, $#, $_, $-, ...) that set -- or a
// function's arguments can set to anything, or a variable the line did not set to plain words. An
// argument of kubectl or helm like that can split into -n, --context or --kubeconfig and override
// the ones read.
func (v lineVars) runTime(word string) bool {
	for _, r := range references(word) {
		if r.Name == "" || specialParameter(r.Name) {
			return true
		}
		if kind, set := v[r.Name]; !set || kind != literalValue {
			return true
		}
	}
	return strings.ContainsRune(word, '`')
}

// specialParameter reports a shell special or positional parameter: $@, $*, $1..$9, $#, $?, $!, $-,
// $$, $0 and bash's $_. Their values are not known before the command runs.
func specialParameter(ref string) bool {
	return len(ref) == 1 && strings.Contains("0123456789@*#?$!-_", ref)
}

// kubeTarget reads the target a kubectl or helm command names and whether it mutates.
func kubeTarget(prog string, tokens []string) (KubeTarget, bool) {
	t := KubeTarget{Kubeconfig: KubeconfigFlag(tokens), Helm: prog == "helm"}
	var res Result
	if t.Helm {
		res = Helm(tokens)
		t.Context, t.Namespace = HelmTargets(tokens)
	} else {
		res = Kubectl(first(tokens), tail(tokens))
		t.Context, t.Namespace = KubeTargets(tokens)
	}
	return t, res.Classification == policy.Mutate
}

// applyEnv applies the variables set for the command. KUBECONFIG names its kubeconfig when no
// --kubeconfig does; HELM_NAMESPACE and HELM_KUBECONTEXT name a helm command's namespace and context
// when no flag does (kubectl ignores them). unknown is true when the command's configuration cannot
// be read: an empty KUBECONFIG (unset for kubectl, so the default file, whatever taracode's own
// environment says), or another variable outside the safe ones (PATH, HOME, ...), which can change
// which kubectl runs or which kubeconfig it finds. An empty HELM_NAMESPACE or HELM_KUBECONTEXT is
// unset for helm: the value then comes from the kubeconfig, not known here, so it is "*".
func (t *KubeTarget) applyEnv(env []string) (unknown bool) {
	flagConfig, flagContext, flagNamespace := t.Kubeconfig, t.Context, t.Namespace
	emptyConfig := false
	for _, a := range env {
		name, value, _ := strings.Cut(a, "=")
		switch {
		case name == "KUBECONFIG":
			if flagConfig == "" {
				t.Kubeconfig, emptyConfig = value, value == ""
			}
		case t.Helm && name == "HELM_NAMESPACE":
			if flagNamespace == "" {
				t.Namespace = orAll(value)
			}
		case t.Helm && name == "HELM_KUBECONTEXT":
			if flagContext == "" {
				t.Context = orAll(value)
			}
		case !t.Helm && strings.HasPrefix(name, "HELM_"):
		case !safeAssignment(name):
			unknown = true
		}
	}
	return unknown || emptyConfig
}

// orAll is value, or "*" for an empty one.
func orAll(value string) string {
	if value == "" {
		return "*"
	}
	return value
}

// relativeKubeconfig reports a kubeconfig path (or list) with a part the working directory resolves.
func relativeKubeconfig(kubeconfig string) bool {
	for _, p := range strings.Split(kubeconfig, ":") {
		if relativePath(p) {
			return true
		}
	}
	return false
}

// otherCommand reads a command that is not kubectl or helm and reports whether it may change the
// kube configuration for the commands after it: a context switcher (kubectx, kubens, kubie), a
// cloud command that writes a cluster's credentials into the kubeconfig, an export or unset of a
// variable outside the safe ones, and a program the classifier does not know or that runs code (a
// script, sh, eval, source, python, make, find -exec), which is also something it cannot see into.
// A container (docker run) is too, but it cannot change the kubeconfig. cd, pushd and popd change
// only the directory a relative kubeconfig is read from.
func (l *kubeLine) otherCommand(prog string, w wrapped) {
	rest := w.words[1:]
	switch {
	case prog == "cd" || prog == "pushd" || prog == "popd":
		l.moved = true
	case prog == "set":
		if len(rest) > 0 { // set -- ... sets the positional parameters, set -k the keyword mode
			l.noteChanged(causeContextSwitch)
		}
	case prog == "export" || prog == "unset":
		if !neutralNames(operands(rest)) {
			l.noteChanged(exportCause(operands(rest)))
		}
	case switchesKubeContext(prog, rest):
		l.noteChanged(causeContextSwitch)
	case !knownProgram(w.words) || runsCode(prog, rest):
		l.opaque = true
		l.noteChanged(causeUnknownProg)
	case containerPrograms[prog]:
		if dockerRunsContainer(rest) { // only a container that runs a command can run a hidden kubectl
			l.opaque = true
		}
	}
}

// exportCause names why an assignment, export or unset earlier on the line makes a later target "*":
// a KUBECONFIG change points at the kubeconfig, any other variable at what kubectl reads or runs.
func exportCause(names []string) string {
	for _, w := range names {
		if name, _, _ := strings.Cut(w, "="); name == "KUBECONFIG" {
			return causeKubeconfigSet
		}
	}
	return causeEnvVariable
}

// dockerRunsContainer reports the docker (podman, nerdctl) commands that run a command in a container,
// where a kubectl the classifier cannot see may act on a cluster: run and exec, container run and
// exec, compose run and exec. Every other verb (pull, tag, push, ps, images, ...) touches no cluster.
func dockerRunsContainer(rest []string) bool {
	verb, after, ok := dockerGlobals.splitVerb(rest)
	if !ok {
		return true // an unknown global before the verb: fail closed
	}
	sub := first(positionals(after, composeValueFlags...))
	switch verb {
	case "run", "exec":
		return true
	case "container", "compose":
		return in(sub, "run", "exec")
	}
	return false
}

// neutralNames reports NAME=value (or NAME) words whose variables cannot change which kubectl or
// helm runs or which cluster it reads: the safe assignments, KUBECONFIG excepted.
func neutralNames(words []string) bool {
	for _, w := range words {
		name, _, _ := strings.Cut(w, "=")
		if name == "KUBECONFIG" || !safeAssignment(name) {
			return false
		}
	}
	return true
}

// switchesKubeContext reports the context switchers and the cloud commands that write a cluster's
// credentials into the kubeconfig and make its context the current one.
func switchesKubeContext(prog string, rest []string) bool {
	switch prog {
	case "kubectx", "kubens", "kubie", "kubeswitch":
		return true
	case "aws":
		pos := positionals(rest, awsValueFlags...)
		return len(pos) > 1 && pos[0] == "eks" && pos[1] == "update-kubeconfig"
	case "az":
		return in("get-credentials", positionals(rest, azValueFlags...)...)
	case "gcloud":
		return in("get-credentials", positionals(rest, gcloudValueFlags...)...)
	}
	return false
}

// kubectlChangesConfig reports a kubectl command that changes the kubeconfig or its current context:
// a config subcommand other than a read, the ctx and ns plugins (kubectx and kubens), cp into a
// kubeconfig, or an option before the verb the classifier does not read past.
func kubectlChangesConfig(tokens, named []string) bool {
	verb, rest, ok := kubectlGlobals.splitVerb(tokens)
	switch {
	case !ok:
		return true
	case verb == "config":
		return Kubectl(verb, rest).Classification == policy.Mutate
	case verb == "ctx" || verb == "ns" || verb == "konfig":
		return true
	case verb == "cp":
		ops := operands(rest, "-c", "--container", "--retries")
		return len(ops) > 0 && kubeconfigPath(ops[len(ops)-1], named)
	}
	return false
}

// kubectlRunsCommands reports the kubectl verbs that run a command in a container, where a kubectl
// the classifier cannot see may act on the cluster.
func kubectlRunsCommands(tokens []string) bool {
	verb, _, _ := kubectlGlobals.splitVerb(tokens)
	return in(verb, "exec", "debug", "run", "attach")
}

// codeRunners are the programs the classifier knows that run code it does not see: the language
// runtimes, make's recipes and the program arch is given. find runs one with -exec and -ok.
var codeRunners = map[string]bool{"python": true, "python2": true, "python3": true, "node": true, "ruby": true,
	"perl": true, "php": true, "java": true, "deno": true, "bun": true, "make": true, "arch": true}

// containerPrograms run commands in a container: a kubectl there acts on a cluster the classifier
// cannot tell.
var containerPrograms = map[string]bool{"docker": true, "podman": true, "nerdctl": true}

func runsCode(prog string, rest []string) bool {
	return codeRunners[prog] || prog == "find" && hasFlag(rest, "-exec", "-execdir", "-ok", "-okdir")
}

// kubeNeutralPrograms are shell builtins and commands with no classifier rule that cannot change
// the kube configuration: a guard such as [ -f x.yaml ] && kubectl apply -f x.yaml keeps its target.
var kubeNeutralPrograms = map[string]bool{"mkdir": true, "set": true, ":": true, "wait": true, "[": true,
	"[[": true}

// knownProgram reports whether the classifier has a rule for the program a command runs.
func knownProgram(words []string) bool {
	prog, ok := programName(words[0])
	if !ok {
		return false
	}
	rest := words[1:]
	if kubeNeutralPrograms[prog] || readOnlyPrograms[prog] {
		return true
	}
	if _, ok := subcommandReads[prog]; ok {
		return true
	}
	if _, ok := fileWriters[prog]; ok {
		return true
	}
	if _, ok := shellDevopsTool(prog, rest); ok {
		return true
	}
	if _, ok := shellFileTool(prog, rest); ok {
		return true
	}
	_, ok = shellMiscTool(prog, rest)
	return ok
}

// writesKubeconfig reports a segment that writes a kubeconfig, through a redirect or a file-writing
// program: kubectl reads the new file, not the one the classifier saw.
func (l *kubeLine) writesKubeconfig(words, redirects []string) bool {
	var targets []string
	if len(words) > 0 {
		targets = writtenOperands(words[assignmentsEnd(words):])
	}
	for _, r := range redirects {
		if writesFile(r) {
			_, target, _ := strings.Cut(r, " ")
			targets = append(targets, target)
		}
	}
	for _, p := range targets {
		if kubeconfigPath(p, l.named) {
			return true
		}
	}
	return false
}

// kubeconfigPath reports a path that is or may be a kubeconfig: under a .kube directory, named like
// one, $KUBECONFIG, or a kubeconfig the line names.
func kubeconfigPath(p string, named []string) bool {
	clean := path.Clean(p)
	if strings.Contains("/"+clean+"/", "/.kube/") || strings.Contains(strings.ToLower(path.Base(clean)), "kubeconfig") ||
		strings.Contains(p, "KUBECONFIG") {
		return true
	}
	for _, n := range named {
		if n != "" && path.Clean(n) == clean {
			return true
		}
	}
	return false
}

// namedKubeconfigs returns the kubeconfig files a line names anywhere: KUBECONFIG= values (each path
// of a list) and --kubeconfig values.
func namedKubeconfigs(segments []shellwords.Segment) []string {
	var out []string
	for _, seg := range segments {
		for i, w := range seg.Words {
			switch {
			case strings.HasPrefix(w, "KUBECONFIG="):
				out = append(out, strings.Split(strings.TrimPrefix(w, "KUBECONFIG="), ":")...)
			case strings.HasPrefix(w, "--kubeconfig="):
				out = append(out, strings.TrimPrefix(w, "--kubeconfig="))
			case w == "--kubeconfig" && i+1 < len(seg.Words):
				out = append(out, seg.Words[i+1])
			}
		}
	}
	return out
}

// countKubeWords counts the words that name kubectl or helm: the program word, a path to it, an
// image such as bitnami/kubectl:1.30, or the word after a backtick or a glued brace.
func countKubeWords(words []string) int {
	n := 0
	for _, w := range words {
		name := path.Base(strings.TrimLeft(w, "{`"))
		if i := strings.IndexAny(name, ":@"); i > 0 {
			name = name[:i]
		}
		if name == "kubectl" || name == "helm" {
			n++
		}
	}
	return n
}
