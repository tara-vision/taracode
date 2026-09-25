package classify

import "strings"

// causeExpandedObjects is why a kubectl command of a shell line whose words sh expands has the
// namespace "*" (expandsIntoNamespaceObject).
const causeExpandedObjects = "a word the shell expands can name a namespace object"

// expandsIntoNamespaceObject reports a kubectl command of a shell line that holds a word sh expands (a
// glob, or a brace with alternatives or a sequence) and can change a namespace object through it.
// Brace expansion adds words anywhere: it can name the namespace kind ({ns,kube-system} is ns
// kube-system), add a name to a namespace object (delete ns shop --grace-period {0,kube-system}) or
// move the verb (kubectl {delete,ns} kube-system), and a glob can match a file named ns or
// kube-system. So a verb that changes objects acts on any namespace when a word of the namespace kind
// is among its objects, as written or once expanded, or an option's value expands to one; with the
// verb itself unreadable, when any word can name the kind. The words have their quotes removed, so a
// quoted glob or brace counts too (fail closed). The kubectl tool runs no shell and is not read so.
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
	if !namespaceObjectVerbs[strings.ToLower(verb)] {
		return false
	}
	args := readKubeArgs(rest)
	if _, found := starIfNamespaceWord(append(append([]string{}, args.positionals...), after...)); found {
		return true
	}
	for _, v := range args.values {
		if expandsToNamespaceKind(v) {
			return true
		}
	}
	return false
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
