package classify

import (
	"path"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
)

// causeNamespaceObjects is why the namespace of a command that changes namespace objects is "*"; the
// policy names its own remedy for it.
const causeNamespaceObjects = policy.CauseNamespaceObjects

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

// KubeTargetsWithCause reads the context and the namespace a kubectl command acts on: its --context
// and -n (KubeTargets), and a namespace it changes as an object. The namespace object is the target;
// with a -n that names another namespace, several namespace objects or a selection of them, it is "*",
// and cause is policy.CauseNamespaceObjects, for the deny to name with its remedy.
func KubeTargetsWithCause(tokens []string) (context, namespace, cause string) {
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
// words after a lone "--" are objects too: pflag stops reading options there, not arguments. The
// changes of label and annotate are not objects (withoutChanges). The words are read as kubectl gets
// them; on a shell line, a word sh expands is read by expandsIntoNamespaceObject.
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
	objects = withoutChanges(verb, objects)
	if len(objects) == 0 {
		return "", false
	}
	names, namespaces, others := namespaceObjects(objects)
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

// withoutChanges drops the changes label and annotate take after their objects, split off the way
// kubectl splits them (GetResourcesAndPairs): the first KEY=VALUE with the "=" not first, or KEY- other
// than a lone "-", ends the objects, and kubectl refuses an object after a change. So "-" and "=a" are
// objects: kubectl label ns - kube-system team=x asks for the namespace "-" and relabels kube-system.
func withoutChanges(verb string, objects []string) []string {
	if verb != "label" && verb != "annotate" {
		return objects
	}
	for i, o := range objects {
		if kubeChange(o) {
			return objects[:i]
		}
	}
	return objects
}

// kubeChange is kubectl's test for a label or annotate change rather than an object: KEY=VALUE, or
// KEY- to remove one.
func kubeChange(word string) bool {
	return (strings.Contains(word, "=") && word[0] != '=') || (strings.HasSuffix(word, "-") && word != "-")
}

// namespaceObjects reads objects the way kubectl does: type/name words, or a type (or a comma list of
// types) and then names. It returns the names of the namespace objects, and whether there is a
// namespace and something of another kind.
func namespaceObjects(objects []string) (names []string, namespaces, others bool) {
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
	return append(names, objects[1:]...), namespaces, others
}

// namespaceKind reports a resource word of the namespace kind in any spelling: ns, namespace or
// namespaces, in any case, with or without a group or version after a dot.
func namespaceKind(word string) bool {
	kind, _, _ := strings.Cut(word, ".")
	return policy.CanonicalKubeResource(kind) == "namespace"
}

// starIfNamespaceWord is "*" when a word other than an option could name the namespace kind (ns,
// namespace/x, pod,ns), as written or once sh expands it (a brace leaf: n{s,}; a glob that can match
// a file named like the kind: n?): the objects cannot be read exactly, so the command may change a
// namespace.
func starIfNamespaceWord(words []string) (string, bool) {
	for _, w := range words {
		if looksLikeFlag(w) {
			continue
		}
		if _, _, found := braceLeaves(w, braceSequence, namespaceTypeWord); found || namespaceTypeWord(w) {
			return "*", true
		}
	}
	return "", false
}

// namespaceTypeWord reports a word whose type part names the namespace kind (ns, namespace/x, pod,ns),
// or is a glob that can match a file named so.
func namespaceTypeWord(w string) bool {
	kind, _, _ := strings.Cut(w, "/")
	for _, k := range strings.Split(kind, ",") {
		if namespaceKind(k) || globMatchesNamespaceKind(k) {
			return true
		}
	}
	return false
}

// globMatchesNamespaceKind reports a glob that can match a file named ns, namespace or namespaces, in
// any case and with a group or version after a dot: sh replaces it with the names of the files it
// matches in the working directory.
func globMatchesNamespaceKind(k string) bool {
	if !strings.ContainsAny(k, "*?[") {
		return false
	}
	lower := strings.ToLower(k)
	head, _, _ := strings.Cut(lower, ".")
	for _, spelling := range []string{"ns", "namespace", "namespaces"} {
		whole, _ := path.Match(lower, spelling)
		beforeDot, _ := path.Match(head, spelling)
		if whole || beforeDot {
			return true
		}
	}
	return false
}

// kubeArgs are the words after a kubectl verb, read as kubectl reads them.
type kubeArgs struct {
	positionals []string
	values      []string // the words options take as their values (-l x, --grace-period 0)
	selects     bool     // --all, -l or --field-selector: the command selects objects instead of naming them
	raw         bool     // --raw: a URI names the object, in any namespace
	ambiguous   bool     // before the first positional, an option the classifier does not know: it may take the next word
}

// readKubeArgs separates the objects from the options and their values.
func readKubeArgs(tokens []string) kubeArgs {
	var a kubeArgs
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case strings.HasPrefix(t, "--"):
			i += a.value(tokens, i, a.long(t, len(a.positionals) == 0))
		case looksLikeFlag(t):
			i += a.value(tokens, i, a.short(t, len(a.positionals) == 0))
		default:
			a.positionals = append(a.positionals, t)
		}
	}
	return a
}

// value records the word after the option at tokens[i] when the option takes it as its value (n is 1),
// and returns n.
func (a *kubeArgs) value(tokens []string, i, n int) int {
	if n == 1 && i+1 < len(tokens) {
		a.values = append(a.values, tokens[i+1])
	}
	return n
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
