package classify

import (
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
)

// causeNamespaceObjects is why the namespace of a command that changes namespace objects is "*".
const causeNamespaceObjects = "the command changes several namespaces or selects them, or names a namespace " +
	"object and another namespace"

// namespaceObjectVerbs change the objects they name, so a namespace they name as an object is what
// they act on (ruling P3-R69): kubectl delete ns kube-system deletes kube-system whatever -n says.
var namespaceObjectVerbs = map[string]bool{"delete": true, "edit": true, "patch": true, "replace": true,
	"apply": true, "annotate": true, "label": true, "create": true}

// applyObjectSubcommands come before the objects of kubectl apply: apply edit-last-applied ns x edits x.
var applyObjectSubcommands = []string{"edit-last-applied", "set-last-applied", "view-last-applied"}

// kubeValuedLong are long options of the verbs that change objects which take the next word as their
// value when it is not glued with "="; the global ones are kubectlGlobals'. The value is never an object.
var kubeValuedLong = []string{"--field-selector", "--filename", "--grace-period", "--kustomize", "--output",
	"--raw", "--selector", "--timeout", "--field-manager", "--subresource", "--template", "--patch", "--patch-file",
	"--type", "--resource-version", "--prune-allowlist", "--prune-whitelist", "--chunk-size", "--cache-dir",
	"--profile", "--profile-output", "--log-flush-frequency", "--for", "--label-columns", "--sort-by"}

// kubeBooleanLong are long options of those verbs that never take the next word: booleans, and the
// options whose bare form has a default value (--cascade, --dry-run, --validate).
var kubeBooleanLong = []string{"--all", "--all-namespaces", "--force", "--ignore-not-found", "--interactive",
	"--now", "--recursive", "--wait", "--allow-missing-template-keys", "--output-patch", "--save-config",
	"--show-managed-fields", "--windows-line-endings", "--local", "--list", "--overwrite", "--edit",
	"--force-conflicts", "--prune", "--server-side", "--record", "--cascade", "--dry-run", "--validate",
	"--openapi-patch", "--show-labels", "--no-headers"}

// kubeObjectTargets reads the context and the namespace a kubectl command acts on: its --context and
// -n (KubeTargets), and a namespace it changes as an object. The namespace object is the target; with
// a -n that names another namespace, several namespace objects or a selection of them, it is "*", and
// cause says why.
func kubeObjectTargets(tokens []string) (context, namespace, cause string) {
	f := parseKubeFlags(tokens, "--context")
	context, namespace = oneValue(f.contexts), f.namespace()
	object, ok := namespaceObject(tokens)
	switch {
	case !ok:
		return context, namespace, ""
	case object == "*" || (namespace != "" && namespace != object):
		return context, "*", causeNamespaceObjects
	}
	return context, object, ""
}

// namespaceObject reads the namespace a kubectl command changes as an object (delete ns x, delete
// namespace/x, label ns x k=v). ok is true when a verb that changes objects names an object of the
// namespace kind, or may: target is then the one namespace it names, or "*" when it names several,
// selects them (--all, -l, --field-selector), mixes them with objects of another kind, names them in a
// raw URI (--raw), or cannot be read exactly (an option the classifier does not know, before the verb
// or the objects, may take the next word as its value, and a word of the namespace kind follows). The
// words after a lone "--" are objects too: pflag stops reading options there, not arguments.
func namespaceObject(tokens []string) (target string, ok bool) {
	before, after := cutDoubleDash(tokens)
	verb, rest, known := kubectlGlobals.splitVerb(before)
	if !known {
		return starIfNamespaceWord(tokens)
	}
	verb = strings.ToLower(verb)
	if !namespaceObjectVerbs[verb] {
		return "", false
	}
	args := readKubeArgs(rest)
	if args.raw {
		return "*", true
	}
	objects := append(append([]string{}, args.positionals...), after...)
	if verb == "apply" && len(objects) > 0 && in(objects[0], applyObjectSubcommands...) {
		objects = objects[1:]
	}
	if args.ambiguous {
		return starIfNamespaceWord(objects)
	}
	if len(objects) == 0 {
		return "", false
	}
	names, namespaces, others := namespaceObjects(verb, objects)
	switch {
	case !namespaces:
		return "", false
	case others || args.selects:
		return "*", true
	}
	target = oneValue(names)
	return target, target != "" // kubectl refuses a namespace object without a name
}

// cutDoubleDash splits tokens at the first lone "--": the words before it, and the words after it,
// which kubectl reads as arguments whatever they look like.
func cutDoubleDash(tokens []string) (before, after []string) {
	for i, t := range tokens {
		if t == "--" {
			return tokens[:i], tokens[i+1:]
		}
	}
	return tokens, nil
}

// namespaceObjects reads objects the way kubectl does: type/name words, or a type (or a comma list of
// types) and then names, which for label and annotate end at the first KEY=VALUE or KEY-. It returns
// the names of the namespace objects, and whether there is a namespace and something of another kind.
func namespaceObjects(verb string, objects []string) (names []string, namespaces, others bool) {
	if strings.Contains(objects[0], "/") {
		for _, o := range objects {
			kind, name, slashed := strings.Cut(o, "/")
			switch {
			case !slashed || !namespaceKind(kind):
				others = true
			case strings.Contains(name, "/"): // not a name kubectl sends; read it as more than one
				namespaces, others = true, true
			default:
				namespaces = true
				names = append(names, name)
			}
		}
		return names, namespaces, others
	}
	for _, kind := range strings.Split(objects[0], ",") {
		if namespaceKind(kind) {
			namespaces = true
		} else {
			others = true
		}
	}
	for _, o := range objects[1:] {
		if (verb == "label" || verb == "annotate") && (strings.Contains(o, "=") || strings.HasSuffix(o, "-")) {
			break
		}
		names = append(names, o)
	}
	return names, namespaces, others
}

// namespaceKind reports a resource word of the namespace kind in any spelling: ns, namespace or
// namespaces, in any case, with or without a group or version after a dot.
func namespaceKind(word string) bool {
	kind, _, _ := strings.Cut(word, ".")
	return policy.CanonicalKubeResource(kind) == "namespace"
}

// starIfNamespaceWord is "*" when a word other than an option could name the namespace kind (ns,
// namespace/x, pod,ns): the objects cannot be read exactly, so the command may change a namespace.
func starIfNamespaceWord(words []string) (string, bool) {
	for _, w := range words {
		if looksLikeFlag(w) {
			continue
		}
		kind, _, _ := strings.Cut(w, "/")
		for _, k := range strings.Split(kind, ",") {
			if namespaceKind(k) {
				return "*", true
			}
		}
	}
	return "", false
}

// kubeArgs are the words after a kubectl verb, read as kubectl reads them.
type kubeArgs struct {
	positionals []string
	selects     bool // --all, -l or --field-selector: the command selects objects instead of naming them
	raw         bool // --raw: a URI names the object, in any namespace
	ambiguous   bool // before the first positional, an option the classifier does not know: it may take the next word
}

// readKubeArgs separates the objects from the options and their values.
func readKubeArgs(tokens []string) kubeArgs {
	var a kubeArgs
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case strings.HasPrefix(t, "--"):
			i += a.long(t, len(a.positionals) == 0)
		case looksLikeFlag(t):
			i += a.short(t, len(a.positionals) == 0)
		default:
			a.positionals = append(a.positionals, t)
		}
	}
	return a
}

// long reads the long option t and returns how many words after it its value takes.
func (a *kubeArgs) long(t string, beforeObjects bool) int {
	name, _, inline := strings.Cut(t, "=")
	if in(name, "--all", "--selector", "--field-selector") {
		a.selects = true
	}
	a.raw = a.raw || name == "--raw"
	switch {
	case inline, in(name, kubeBooleanLong...), in(name, kubectlGlobals.boolean...):
		return 0
	case in(name, kubeValuedLong...), in(name, kubectlGlobals.valued...):
		return 1
	}
	a.ambiguous = a.ambiguous || beforeObjects
	return 0
}

// short reads a cluster of short options (-R, -f x, -fx, -l=x, -Rf x) and returns how many words after
// it its value takes: a valued letter takes the rest of the word, or the next word when it ends it.
func (a *kubeArgs) short(t string, beforeObjects bool) int {
	for j := 1; j < len(t); j++ {
		c := t[j]
		switch {
		case strings.IndexByte(kubeShortBool, c) >= 0:
		case strings.IndexByte(kubeShortValued, c) >= 0:
			if c == 'l' {
				a.selects = true
			}
			if j+1 < len(t) {
				return 0
			}
			return 1
		default:
			a.ambiguous = a.ambiguous || beforeObjects
			return 0
		}
	}
	return 0
}
