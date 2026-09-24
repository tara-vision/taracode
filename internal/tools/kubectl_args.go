package tools

import (
	"fmt"
	"strings"

	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// KubectlArgv is the argv the kubectl tool runs for its parameters: the verb, the resource and the
// name, then -n, --context and -o from their parameters, then the extra flags of args. The parameters
// are read the way kubectl reads its arguments (ruling P3-R64): one that holds several words (resource
// "pod x") is those words, and a type,name word at the head of the command (pod,x, which kubectl reads
// as two resource types and refuses) is type/name. A model that repeats the command line in args while
// it also fills the parameters (verb get, resource pods, namespace billing, args "get pods -n billing")
// is normalized first (ruling P3-R59): a leading kubectl and the leading copies of the verb, the
// resource and the name come off args, and a namespace, context or output that args repeats with the
// parameter's value is merged. What is still wrong is an argument error, the text the tool refuses the
// call with: args that start with another kubectl command, or a namespace, context or output given
// twice with different values. The eval signature keys this argv, so a replayed call meets the fixture
// of the command it would run.
func KubectlArgv(params map[string]any) ([]string, error) {
	if _, err := required(params, "verb"); err != nil {
		return nil, err
	}
	extra, err := shellwords.Words(argString(params, "args"))
	if err != nil {
		return nil, err
	}
	head := readKubectlHead(params)
	if extra, err = dropRepeatedCommand(extra, head.verb, head.resource, head.name); err != nil {
		return nil, err
	}
	argv, extra := typeNameAtHead(head, extra)
	for _, f := range kubectlFlagParams {
		value := argString(params, f.param)
		if value == "" {
			continue
		}
		if extra, err = mergeRepeatedFlag(extra, f, value); err != nil {
			return nil, err
		}
		argv = append(argv, f.flag(), value)
	}
	return append(argv, extra...), nil
}

// kubectlHead is the start of the command the verb, resource and name parameters spell: their words in
// that order, and the verb, resource and name the copies in args are matched against.
type kubectlHead struct {
	words                []string
	verb, resource, name string
}

// readKubectlHead reads the verb, resource and name parameters the way kubectl reads its arguments. A
// parameter that holds several words (resource "pod x", verb "describe pod") is those words as
// separate arguments, since kubectl refuses "pod x" as one; the words then fill the verb, the resource
// and the name in order. With one word per parameter each stays in its own slot.
func readKubectlHead(params map[string]any) kubectlHead {
	verb, resource, name := argString(params, "verb"), argString(params, "resource"), argString(params, "name")
	var words []string
	slots := 0
	for _, p := range []string{verb, resource, name} {
		if p != "" {
			slots++
			words = append(words, strings.Fields(p)...)
		}
	}
	if len(words) == slots {
		return kubectlHead{words: words, verb: verb, resource: resource, name: name}
	}
	return kubectlHead{words: words, verb: words[0], resource: wordAt(words, 1), name: wordAt(words, 2)}
}

// wordAt is words[i], or "" past the end.
func wordAt(words []string, i int) string {
	if i < len(words) {
		return words[i]
	}
	return ""
}

// typeNameAtHead starts the argv with the head's words and rewrites a type,name word at the head of the
// command, the first word after the verb or, when no parameter names the object, the first word of
// args, to type/name (typeName). A verb given as a global flag heads a command line of its own, which
// is left as written.
func typeNameAtHead(head kubectlHead, extra []string) (argv, rest []string) {
	argv = append([]string{}, head.words...)
	switch {
	case strings.HasPrefix(head.verb, "-"):
	case len(argv) > 1:
		argv[1] = typeName(argv[1])
	case len(extra) > 0 && !strings.HasPrefix(extra[0], "-"):
		extra = append([]string{typeName(extra[0])}, extra[1:]...)
	}
	return argv, extra
}

// typeName is type/name for a type,name word whose first part is a resource type and whose second is
// not: kubectl reads pod,x as a list of two resource types and refuses x, so the model meant the pod
// named x. Any other word, a list of resource types (pods,services) included, stays as it is.
func typeName(w string) string {
	kind, name, ok := strings.Cut(w, ",")
	if !ok || name == "" || strings.ContainsAny(name, ",/") || strings.Contains(kind, "/") ||
		!isKubeResourceWord(kind) || isKubeResourceWord(name) {
		return w
	}
	return kind + "/" + name
}

// dropRepeatedCommand takes off args the command line a model repeats there: a leading kubectl that
// more words follow, then the verb, then the resource and the name when those parameters are set.
// Args that then start with another kubectl command are an argument error, since args holds only
// extra flags. A verb given as a global flag (verb "-n", args "kube-system delete pod x") makes args
// the rest of a command line of its own, which is left as written.
func dropRepeatedCommand(words []string, verb, resource, name string) ([]string, error) {
	if strings.HasPrefix(verb, "-") {
		return words, nil
	}
	if len(words) > 1 && words[0] == "kubectl" {
		words = words[1:]
	}
	if len(words) > 0 && words[0] == verb {
		words = words[1:]
	}
	words = dropRepeatedObject(words, resource, name)
	if len(words) > 0 && words[0] != verb && kubectlCommands[words[0]] && !kubectlSubcommandVerbs[verb] {
		return nil, fmt.Errorf("args starts with %q but verb is %q; args holds only extra flags "+
			"(for example -l app=web --tail=100), never the verb, resource, name, namespace or context", words[0], verb)
	}
	return words, nil
}

// dropRepeatedObject takes off a leading copy of the resource parameter, then one of the name. A
// type/name resource parameter (deploy/web) names the object as well.
func dropRepeatedObject(words []string, resource, name string) []string {
	kind, objectName, _ := strings.Cut(resource, "/")
	if name == "" {
		name = objectName
	}
	if len(words) > 0 && resource != "" {
		words = dropRepeatedResource(words, kind, name)
	}
	if len(words) > 0 && name != "" && words[0] == name {
		words = words[1:]
	}
	return words
}

// dropRepeatedResource takes off words[0] when it repeats the resource type kind, alone or as
// type/name: pods repeats pod, and pod/x repeats pod with the name x. When no parameter names the
// object, the x of pod/x stays as the name kubectl reads; a type/name with another name than the
// parameters give is left for kubectl to refuse.
func dropRepeatedResource(words []string, kind, name string) []string {
	wordKind, wordName, slashed := strings.Cut(words[0], "/")
	switch {
	case !sameKubeResource(wordKind, kind):
		return words
	case !slashed || wordName == name:
		return words[1:]
	case name == "":
		return append([]string{wordName}, words[1:]...)
	}
	return words
}

// kubectlFlagParam is a parameter the kubectl tool passes as a flag, with the spellings args can
// repeat it in.
type kubectlFlagParam struct {
	param, short, long string // short is "" for a flag that has no short form
}

// kubectlFlagParams are the parameters the tool passes as flags, in the order it passes them.
var kubectlFlagParams = []kubectlFlagParam{
	{param: "namespace", short: "-n", long: "--namespace"},
	{param: "context", long: "--context"},
	{param: "output", short: "-o", long: "--output"},
}

// flag is the spelling the tool passes the parameter with.
func (f kubectlFlagParam) flag() string {
	if f.short != "" {
		return f.short
	}
	return f.long
}

// flagCopy is one copy of a flag parameter in args.
type flagCopy struct {
	value   string
	span    int  // the words it takes: 2 when the value is the next word
	noValue bool // the flag ends args with no value after it
}

// copyAt reads the copy of the flag at words[i], when there is one: -n x, -n=x, -nx, --namespace x or
// --namespace=x, the forms kubectl reads.
func (f kubectlFlagParam) copyAt(words []string, i int) (flagCopy, bool) {
	w := words[i]
	switch {
	case w == f.long || (f.short != "" && w == f.short):
		if i+1 == len(words) {
			return flagCopy{span: 1, noValue: true}, true
		}
		return flagCopy{value: words[i+1], span: 2}, true
	case strings.HasPrefix(w, f.long+"="):
		return flagCopy{value: strings.TrimPrefix(w, f.long+"="), span: 1}, true
	case f.short != "" && strings.HasPrefix(w, f.short):
		return flagCopy{value: strings.TrimPrefix(strings.TrimPrefix(w, f.short), "="), span: 1}, true
	}
	return flagCopy{}, false
}

// mergeRepeatedFlag takes off args every copy of a flag parameter that carries the parameter's value,
// so the flag is passed once, from the parameter. A copy with another value, or with none, is an
// argument error that names both. Words after a lone "--" are the command a container runs, never
// kubectl's flags, and stay as written.
func mergeRepeatedFlag(words []string, f kubectlFlagParam, value string) ([]string, error) {
	kept := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		if words[i] == "--" {
			return append(kept, words[i:]...), nil
		}
		c, ok := f.copyAt(words, i)
		if !ok {
			kept = append(kept, words[i])
			continue
		}
		if c.noValue {
			return nil, fmt.Errorf("%s is given both as a parameter (%q) and in args, where %s has no value; use one",
				f.param, value, words[i])
		}
		if c.value != value {
			return nil, fmt.Errorf("%s is given both as a parameter (%q) and in args (%q) with different values; "+
				"use one", f.param, value, c.value)
		}
		i += c.span - 1
	}
	return kept, nil
}

// kubectlCommands are kubectl's top-level commands. One of them at the start of args, other than the
// verb, is a command line pasted into args with another verb. events is left out: it is also a
// resource type (kubectl get events).
var kubectlCommands = map[string]bool{
	"create": true, "expose": true, "run": true, "set": true, "explain": true, "get": true, "edit": true,
	"delete": true, "rollout": true, "scale": true, "autoscale": true, "certificate": true, "cluster-info": true,
	"top": true, "cordon": true, "uncordon": true, "drain": true, "taint": true, "describe": true, "logs": true,
	"attach": true, "exec": true, "port-forward": true, "proxy": true, "cp": true, "auth": true, "debug": true,
	"diff": true, "apply": true, "patch": true, "replace": true, "wait": true, "kustomize": true, "label": true,
	"annotate": true, "completion": true, "alpha": true, "api-resources": true, "api-versions": true,
	"config": true, "plugin": true, "version": true, "options": true, "help": true,
}

// kubectlSubcommandVerbs take a subcommand first that can share a top-level command's name (kubectl
// config set, kubectl help get), so their args are never read as another verb.
var kubectlSubcommandVerbs = map[string]bool{"config": true, "help": true, "alpha": true}

// kubeResourceAliases map the short names and plurals of the common resource types to one canonical
// singular name.
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
// short names and plurals mapped to the singular (po and pods are pod, deploy is deployment). A word
// the table does not know stays as written, lower-cased.
func CanonicalKubeResource(r string) string {
	lower := strings.ToLower(r)
	if c, ok := kubeResourceAliases[lower]; ok {
		return c
	}
	return lower
}

// kubeKinds are the resource type words kubectl knows without custom resources: the canonical names of
// the alias table and the other built-in types, with the short names the table lacks.
var kubeKinds = func() map[string]bool {
	kinds := map[string]bool{}
	for _, c := range kubeResourceAliases {
		kinds[c] = true
	}
	for _, k := range []string{"all", "role", "rolebinding", "clusterrole", "clusterrolebinding", "limitrange",
		"limits", "resourcequota", "quota", "poddisruptionbudget", "pdb", "lease", "priorityclass", "pc",
		"certificatesigningrequest", "csr", "replicationcontroller", "rc", "endpointslice", "ingressclass",
		"runtimeclass", "podtemplate", "controllerrevision", "apiservice", "mutatingwebhookconfiguration",
		"validatingwebhookconfiguration", "volumeattachment", "csidriver", "csinode", "componentstatus", "cs"} {
		kinds[k] = true
	}
	return kinds
}()

// isKubeResourceWord reports whether w names a resource type kubectl knows without custom resources:
// a word of the alias table, another built-in type, or the plural of one.
func isKubeResourceWord(w string) bool {
	c := CanonicalKubeResource(w)
	return kubeKinds[c] || kubeKinds[strings.TrimSuffix(c, "s")] || kubeKinds[strings.TrimSuffix(c, "es")]
}

// sameKubeResource reports whether two resource type words name the same type: the same canonical
// name, or one the plural of the other (kubectl takes certificate and certificates alike).
func sameKubeResource(a, b string) bool {
	ca, cb := CanonicalKubeResource(a), CanonicalKubeResource(b)
	return ca != "" && (ca == cb || ca+"s" == cb || cb+"s" == ca)
}
