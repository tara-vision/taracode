package classify

import (
	"strconv"
	"strings"
)

// globalOptions are the options a CLI accepts before its verb that the classifier reads past:
// valued ones take the next word (or =value, or a short value glued on: -nkube-system), boolean
// ones take none. Only options that pick the cluster, the identity, the namespace, the connection
// or the log level are listed. An option that writes a file or loads code (kubectl --profile-output
// and --cache-dir, helm --repository-cache, docker --config with its plugin directories) is left
// out on purpose: before the verb it keeps the command a mutation.
type globalOptions struct{ valued, boolean []string }

var (
	kubectlGlobals = globalOptions{
		valued: []string{"-n", "--namespace", "--context", "--cluster", "--user", "--kubeconfig", "-s", "--server",
			"--as", "--as-group", "--as-uid", "--request-timeout", "-v", "--v", "--vmodule", "--token",
			"--certificate-authority", "--client-certificate", "--client-key", "--tls-server-name", "--username",
			"--password"},
		boolean: []string{"--insecure-skip-tls-verify", "--match-server-version", "--warnings-as-errors",
			"--disable-compression"},
	}
	helmGlobals = globalOptions{
		valued: []string{"-n", "--namespace", "--kube-context", "--kubeconfig", "--kube-apiserver", "--kube-as-user",
			"--kube-as-group", "--kube-ca-file", "--kube-tls-server-name", "--kube-token", "--burst-limit", "--qps"},
		boolean: []string{"--debug", "--kube-insecure-skip-tls-verify"},
	}
	dockerGlobals = globalOptions{
		valued:  []string{"-c", "--context", "-H", "--host", "-l", "--log-level", "--tlscacert", "--tlscert", "--tlskey"},
		boolean: []string{"-D", "--debug", "--tls", "--tlsverify"},
	}
)

// skip returns the index of the first word that is neither one of the global options nor the value
// of one. ok is false, with the index of the option, when an option before it is not listed: it may
// take the next word as its value, so the verb cannot be told.
func (g globalOptions) skip(tokens []string) (int, bool) {
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !looksLikeFlag(t) {
			return i, true
		}
		name, _, inline := strings.Cut(t, "=")
		switch {
		case in(name, g.boolean...):
		case in(name, g.valued...):
			if !inline {
				i++ // the value is the next word
			}
		case !strings.HasPrefix(t, "--") && in(t[:2], g.valued...):
			// a short option with its value glued on: -nkube-system
		default:
			return i, false
		}
	}
	return len(tokens), true
}

// splitVerb separates the verb of a kubectl, helm or docker command from the global options before
// it. ok is false, with the unknown option as the verb, when an option before the verb is not one
// of the listed globals.
func (g globalOptions) splitVerb(tokens []string) (verb string, rest []string, ok bool) {
	i, ok := g.skip(tokens)
	if !ok {
		return tokens[i], nil, false
	}
	return first(tokens[i:]), tail(tokens[i:]), true
}

// unknownGlobal is the reason for an option before the verb that the classifier does not read past.
func unknownGlobal(program, option string) Result {
	return mutate(option, program+" "+option+" before the verb is an option taracode does not read past (it may "+
		"take the next word, write a file or load code), so the command counts as a mutation")
}

// Short options of kubectl and helm, as the target parsing reads a cluster such as -itn: the
// boolean letters continue the cluster, a valued letter takes the rest of the word or, last in the
// word, the next word. -f is valued for every kubectl and helm verb that mutates (logs -f is a
// read); a letter in neither set may or may not take the rest as its value.
const (
	kubeShortBool   = "ARwitqhagdr"
	kubeShortValued = "nfklLocpesvm"
)

// kubeFlags are the target options of one kubectl or helm command, read the way pflag reads them
// (every value of each option in order, a valued option consuming the next word whatever it looks
// like), from the tokens before a lone "--".
type kubeFlags struct {
	contexts, namespaces, kubeconfigs []string
	all                               bool // -A or --all-namespaces, the last one set
	ambiguous                         bool // an n in a cluster that may be another option's value
}

// parseKubeFlags reads the target options; contextOption is --context (kubectl) or --kube-context
// (helm).
func parseKubeFlags(tokens []string, contextOption string) kubeFlags {
	var f kubeFlags
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "--":
			return f
		case strings.HasPrefix(t, "--"):
			i = f.long(tokens, i, contextOption)
		case len(t) > 1 && t[0] == '-':
			i = f.short(tokens, i)
		}
	}
	return f
}

// long reads the long option at tokens[i] and returns the index of the last word it used.
func (f *kubeFlags) long(tokens []string, i int, contextOption string) int {
	name, value, inline := strings.Cut(tokens[i], "=")
	var values *[]string
	switch name {
	case contextOption:
		values = &f.contexts
	case "--namespace":
		values = &f.namespaces
	case "--kubeconfig":
		values = &f.kubeconfigs
	case "--all-namespaces":
		all, err := strconv.ParseBool(value)
		f.all = !inline || err != nil || all
		return i
	default:
		return i
	}
	if !inline {
		if i+1 >= len(tokens) {
			return i
		}
		i++
		value = tokens[i]
	}
	*values = append(*values, value)
	return i
}

// short reads the cluster of short options at tokens[i] (-n x, -nx, -n=x, -itn x) and returns the
// index of the last word it used.
func (f *kubeFlags) short(tokens []string, i int) int {
	t := tokens[i]
	for j := 1; j < len(t); j++ {
		c := t[j]
		switch {
		case c == 'A':
			f.all = true
		case strings.IndexByte(kubeShortBool, c) >= 0:
		case strings.IndexByte(kubeShortValued, c) >= 0:
			value, next := strings.TrimPrefix(t[j+1:], "="), i
			if t[j+1:] == "" {
				if i+1 >= len(tokens) {
					return i
				}
				next, value = i+1, tokens[i+1]
			}
			if c == 'n' {
				f.namespaces = append(f.namespaces, value)
			}
			return next
		default:
			f.ambiguous = f.ambiguous || strings.IndexByte(t[j+1:], 'n') >= 0
			return i
		}
	}
	return i
}

// namespace is the namespace the options name: "*" for every namespace, two different values or
// an ambiguous cluster, "" for none.
func (f kubeFlags) namespace() string {
	if f.all || f.ambiguous {
		return "*"
	}
	return oneValue(f.namespaces)
}

// oneValue is the value every entry shares, "*" when two differ, and "" for none: kubectl and helm
// apply the last value of a repeated option, and the classifier does not rely on reading the same.
func oneValue(values []string) string {
	for _, v := range values {
		if v != values[0] {
			return "*"
		}
	}
	return first(values)
}

// KubeTargets reads the context and namespace a kubectl command names, from the tokens before a
// lone "--" (after it they belong to the command a container runs). "*" means every namespace (-A),
// two different values of the same option, or a cluster of short options whose n may be another
// option's value. A namespace the command changes as an object (delete ns kube-system, in any
// spelling) is the namespace it acts on (ruling P3-R69), and with a -n that names another namespace,
// several namespace objects or a selection of them the namespace is "*" (kubeObjectTargets).
func KubeTargets(tokens []string) (context, namespace string) {
	context, namespace, _ = kubeObjectTargets(tokens)
	return context, namespace
}

// HelmTargets reads the kube context (--kube-context) and namespace (-n, --namespace; -A and
// --all-namespaces are "*") a helm command names, from the tokens before "--", as KubeTargets does.
func HelmTargets(tokens []string) (context, namespace string) {
	f := parseKubeFlags(tokens, "--kube-context")
	return oneValue(f.contexts), f.namespace()
}

// KubeconfigFlag is the --kubeconfig a kubectl or helm command names before "--"; "*" when it names
// two different files.
func KubeconfigFlag(tokens []string) string {
	return oneValue(parseKubeFlags(tokens, "--context").kubeconfigs)
}
