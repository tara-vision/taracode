package policy

import (
	"path"
	"sort"
	"strings"
)

// kubeResourceAliases map the short names and plurals of the common resource types to one canonical
// singular name. It is the one table the kubectl tool, the classifier, the eval signature and the deny
// patterns read resource words with; it lives here because every one of them imports this package.
var kubeResourceAliases = map[string]string{
	"po": "pod", "pods": "pod", "deploy": "deployment", "deployments": "deployment", "svc": "service",
	"services": "service", "cm": "configmap", "configmaps": "configmap", "ns": "namespace", "namespaces": "namespace",
	"no": "node", "nodes": "node", "ing": "ingress", "ingresses": "ingress", "sts": "statefulset",
	"statefulsets": "statefulset", "ds": "daemonset", "daemonsets": "daemonset", "rs": "replicaset",
	"replicasets": "replicaset", "pvc": "persistentvolumeclaim", "persistentvolumeclaims": "persistentvolumeclaim",
	"pv": "persistentvolume", "persistentvolumes": "persistentvolume", "sa": "serviceaccount",
	"serviceaccounts": "serviceaccount", "ev": "event", "events": "event", "secrets": "secret", "jobs": "job",
	"cj": "cronjob", "cronjobs": "cronjob", "ep": "endpoints", "hpa": "horizontalpodautoscaler",
	"horizontalpodautoscalers": "horizontalpodautoscaler", "netpol": "networkpolicy", "networkpolicies": "networkpolicy",
	"sc": "storageclass", "storageclasses": "storageclass", "crd": "customresourcedefinition",
	"crds": "customresourcedefinition", "customresourcedefinitions": "customresourcedefinition",
}

// CanonicalKubeResource is the canonical name of a resource type word: lower case, with the common
// short names and plurals mapped to the singular (po and pods are pod, ns is namespace). A word the
// table does not know stays as written, lower-cased.
func CanonicalKubeResource(r string) string {
	lower := strings.ToLower(r)
	if c, ok := kubeResourceAliases[lower]; ok {
		return c
	}
	return lower
}

// kubeCanonicalKinds are the canonical names the alias table maps to.
var kubeCanonicalKinds = func() map[string]bool {
	kinds := map[string]bool{}
	for _, c := range kubeResourceAliases {
		kinds[c] = true
	}
	return kinds
}()

// KubeResourceKinds returns the canonical names the alias table maps to, sorted, in a new slice.
func KubeResourceKinds() []string {
	kinds := make([]string, 0, len(kubeCanonicalKinds))
	for c := range kubeCanonicalKinds {
		kinds = append(kinds, c)
	}
	sort.Strings(kinds)
	return kinds
}

// knownKubeResource reports a word the alias table names: one of its short names, plurals or
// canonical names.
func knownKubeResource(word string) bool {
	lower := strings.ToLower(word)
	_, alias := kubeResourceAliases[lower]
	return alias || kubeCanonicalKinds[lower]
}

// canonicalKubeCommand spells the kubectl commands of a collapsed command line one way for the deny
// patterns (ruling P3-R69): the program as kubectl (a path to it is its name), and after it a resource
// alias or plural as its canonical name and a type/name as type then name, so kubectl delete ns/x and
// /usr/local/bin/kubectl delete namespaces x both read as kubectl delete namespace x. Words before the
// first kubectl, flags and words the alias table does not name stay as written.
func canonicalKubeCommand(command string) string {
	words := strings.Fields(command)
	out := make([]string, 0, len(words))
	inKubectl := false
	for _, w := range words {
		switch {
		case path.Base(w) == "kubectl":
			inKubectl = true
			out = append(out, "kubectl")
		case !inKubectl || strings.HasPrefix(w, "-"):
			out = append(out, w)
		default:
			out = append(out, canonicalKubeWord(w)...)
		}
	}
	return strings.Join(out, " ")
}

// canonicalKubeWord spells one word after kubectl: a known resource type canonical, a type/name of a
// known type as the two words, anything else as written.
func canonicalKubeWord(w string) []string {
	kind, name, slashed := strings.Cut(w, "/")
	if !knownKubeResource(kind) {
		return []string{w}
	}
	if slashed {
		return []string{CanonicalKubeResource(kind), name}
	}
	return []string{CanonicalKubeResource(kind)}
}
