package classify

import (
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
)

// causeExpandedObjects is why a kubectl command of a shell line whose words sh expands has the
// namespace "*" (expandsIntoNamespaceObject); the policy names the kubectl tool as its remedy.
const causeExpandedObjects = policy.CauseExpandedObjects

// expandsIntoNamespaceObject reports a kubectl command of a shell line that holds a word sh expands (a
// glob, or a brace with alternatives or a sequence) and can change a namespace object through it.
// Brace expansion adds words anywhere: it can name the namespace kind ({ns,kube-system} is ns
// kube-system), add a name to a namespace object (delete ns shop --grace-period {0,kube-system}) or
// move the verb (kubectl {delete,ns} kube-system), and a glob can match a file named ns or
// kube-system. So a verb that changes objects acts on any namespace when a word of the namespace kind
// is among its objects, as written or once expanded, or an option's value expands to one; with the
// verb itself unreadable, when any word can name the kind. The words have their quotes removed, so a
// quoted glob or brace counts too (fail closed). The kubectl tool runs no shell and is not read so.
// kubectl refuses a resource argument next to -f, --filename, -k or --kustomize, so a command that
// reads its objects from files names none, whatever its words expand to (apply -f *.yaml).
func expandsIntoNamespaceObject(tokens []string) bool {
	if !anyExpands(tokens) {
		return false
	}
	before, after := cutDoubleDash(tokens)
	verb, rest, known := kubectlGlobals.splitVerb(before)
	if !known || anyExpands(before[:len(before)-len(rest)]) {
		_, found := starIfNamespaceWord(tokens)
		return found
	}
	verb = strings.ToLower(verb)
	if !namespaceObjectVerbs[verb] {
		return false
	}
	args := readKubeArgs(rest)
	if args.ambiguous { // an option the classifier does not know may take any next word, -f included
		_, found := starIfNamespaceWord(append(append([]string{}, rest...), after...))
		return found
	}
	if args.files {
		return false
	}
	objects := append(append([]string{}, args.positionals...), after...)
	values := args.values
	if len(objects) > 0 && literalOtherKind(objects[0]) {
		values = values[:args.leading] // an option after the type can add only names of that type
	}
	for _, v := range values {
		if expandsToNamespaceKind(v) {
			return true
		}
	}
	return objectsNameNamespace(verb, objects)
}

// objectsNameNamespace reports objects that can name the namespace kind, as written or once sh expands
// them. create reads the kind from its subcommand alone (create ns x, but create configmap x
// --from-file *.conf), and after a type of another kind written out every word names an object of that
// kind (delete pod -n shop *): kubectl reads no kind from a name, and refuses a type/name word there.
func objectsNameNamespace(verb string, objects []string) bool {
	if len(objects) == 0 {
		return false
	}
	if verb == "create" {
		objects = objects[:1]
	}
	if literalOtherKind(objects[0]) {
		return false
	}
	_, found := starIfNamespaceWord(objects)
	return found
}

// literalOtherKind reports a first object written out, without a glob or a brace, as a bare type of a
// kind other than the namespace (pod, deploy, configmap): the words after it name objects of that kind.
func literalOtherKind(word string) bool {
	return !anyExpands([]string{word}) && !strings.Contains(word, "/") && !namespaceTypeWord(word)
}

// anyExpands reports a word sh expands before the program reads it: a glob character, or a brace with
// alternatives or a sequence.
func anyExpands(words []string) bool {
	for _, w := range words {
		if _, _, _, _, brace := braceSplit(w, braceSequence); brace || strings.ContainsAny(w, "*?[") {
			return true
		}
	}
	return false
}

// expandsToNamespaceKind reports an option's value that sh can turn into a word of the namespace kind,
// which kubectl then reads as an object: a brace leaf ({0,ns}), or a glob in its type part that can
// match a file named so (*). The value as written is the option's, not an object.
func expandsToNamespaceKind(value string) bool {
	if _, _, found := braceLeaves(value, braceSequence, namespaceTypeWord); found {
		return true
	}
	kind, _, _ := strings.Cut(value, "/")
	return strings.ContainsAny(kind, "*?[") && namespaceTypeWord(kind)
}
