package evals

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// Signature is the canonical key of one tool call (spec 5.1, ruling R1): whitespace and quoting
// normalized through shellwords, kubectl arguments in one order with resource aliases resolved,
// and a shell line that is a single simple kubectl, helm, terraform, docker or git command keyed
// like the dedicated tool. Two calls with the same signature replay the same fixture.
func Signature(tool string, args map[string]any) string {
	switch tool {
	case "kubectl":
		return kubectlSignature(kubectlArgv(args))
	case "terraform":
		return terraformSignature(str(args, "command"), str(args, "dir"), words(str(args, "args")))
	case "helm", "git", "docker":
		return strings.TrimSpace(tool + " " + strings.Join(words(str(args, "args")), " "))
	case "cloud":
		return strings.TrimSpace(str(args, "provider") + " " + strings.Join(words(str(args, "args")), " "))
	case "scan":
		return strings.TrimSpace(strings.Join([]string{"scan", str(args, "scanner"), str(args, "target"),
			strings.ToUpper(str(args, "severity"))}, " "))
	case "shell":
		return shellSignature(str(args, "command"))
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{tool}
	for _, k := range keys {
		parts = append(parts, k+"="+strings.TrimSpace(fmt.Sprint(args[k])))
	}
	return strings.Join(parts, " ")
}

// DryRunSignature keys a tool's dry run.
func DryRunSignature(tool string, args map[string]any) string {
	return "dryrun:" + Signature(tool, args)
}

func str(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

// words splits an argument string like the tools do; an unbalanced quote falls back to fields.
func words(s string) []string {
	w, err := shellwords.Words(s)
	if err != nil {
		return strings.Fields(s)
	}
	return w
}

// kubectlArgv rebuilds the argv the kubectl tool runs from its structured parameters, in the same
// order internal/tools builds it, so the dedicated tool and a shell line meet in kubectlSignature.
func kubectlArgv(args map[string]any) []string {
	argv := []string{str(args, "verb")}
	if r := str(args, "resource"); r != "" {
		argv = append(argv, r)
	}
	if n := str(args, "name"); n != "" {
		argv = append(argv, n)
	}
	if ns := str(args, "namespace"); ns != "" {
		argv = append(argv, "-n", ns)
	}
	if c := str(args, "context"); c != "" {
		argv = append(argv, "--context", c)
	}
	if o := str(args, "output"); o != "" {
		argv = append(argv, "-o", o)
	}
	return append(argv, words(str(args, "args"))...)
}

// kubectlValueFlags are the flags the signature lifts into fixed positions.
var kubectlValueFlags = map[string]string{"-n": "namespace", "--namespace": "namespace", "--context": "context",
	"-o": "output", "--output": "output", "--kubeconfig": "kubeconfig"}

// resourceVerbs take a resource type as their first positional; the other verbs (logs, exec, cp,
// port-forward, cordon, ...) take a name, which is never canonicalized.
var resourceVerbs = map[string]bool{"get": true, "describe": true, "delete": true, "edit": true, "patch": true,
	"scale": true, "annotate": true, "label": true, "wait": true, "top": true, "explain": true, "expose": true,
	"autoscale": true, "taint": true, "rollout": false}

var resourceAliases = map[string]string{
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

func canonicalResource(r string) string {
	lower := strings.ToLower(r)
	if c, ok := resourceAliases[lower]; ok {
		return c
	}
	return lower
}

// kubectlSignature renders "kubectl <verb> <resource>[/<name>] [-n ns] [--context c] [-o out]
// [--kubeconfig f] [<extra flags as written>]".
func kubectlSignature(argv []string) string {
	if len(argv) == 0 || argv[0] == "" {
		return "kubectl"
	}
	verb := argv[0]
	positional, extra, fields := parseKubectlArgs(argv[1:])
	parts := []string{"kubectl", verb}
	appendKubectlResourceAndName(&parts, verb, positional)
	appendKubectlFields(&parts, fields)
	return strings.Join(append(parts, extra...), " ")
}

// parseKubectlArgs extracts fields, flags, and positional arguments from kubectl arguments.
func parseKubectlArgs(argv []string) (positional, extra []string, fields map[string]string) {
	fields = map[string]string{}
	for i := 0; i < len(argv); i++ {
		t := argv[i]
		if name, value, ok := strings.Cut(t, "="); ok && kubectlValueFlags[name] != "" {
			fields[kubectlValueFlags[name]] = value
			continue
		}
		if field := kubectlValueFlags[t]; field != "" && i+1 < len(argv) {
			fields[field] = argv[i+1]
			i++
			continue
		}
		if parseKubectlShortFlag(t, fields) {
			continue
		}
		if strings.HasPrefix(t, "-") {
			extra = append(extra, t)
		} else {
			positional = append(positional, t)
		}
	}
	return
}

// parseKubectlShortFlag handles short flags with values like -nshop and -owide.
func parseKubectlShortFlag(t string, fields map[string]string) bool {
	if strings.HasPrefix(t, "-n") && len(t) > 2 && !strings.HasPrefix(t, "--") {
		fields["namespace"] = t[2:]
		return true
	}
	if strings.HasPrefix(t, "-o") && len(t) > 2 && !strings.HasPrefix(t, "--") {
		fields["output"] = t[2:]
		return true
	}
	return false
}

// appendKubectlResourceAndName adds resource and name to parts.
func appendKubectlResourceAndName(parts *[]string, verb string, positional []string) {
	if len(positional) == 0 {
		return
	}
	resource, name, _ := strings.Cut(positional[0], "/")
	rest := positional[1:]
	if name == "" && resourceVerbs[verb] && len(rest) > 0 {
		name, rest = rest[0], rest[1:]
	}
	item := resource
	if resourceVerbs[verb] || name != "" {
		item = canonicalResource(resource)
	}
	if name != "" {
		item += "/" + name
	}
	*parts = append(*parts, item)
	*parts = append(*parts, rest...)
}

// appendKubectlFields adds flags and their values to parts.
func appendKubectlFields(parts *[]string, fields map[string]string) {
	for _, f := range []struct {
		key  string
		flag string
	}{{"namespace", "-n"}, {"context", "--context"}, {"output", "-o"}, {"kubeconfig", "--kubeconfig"}} {
		if v := fields[f.key]; v != "" {
			*parts = append(*parts, f.flag, v)
		}
	}
}

// terraformSignature renders "terraform <command> dir=<clean dir> [<args as written>]".
func terraformSignature(command, dir string, args []string) string {
	if dir == "" {
		dir = "."
	}
	parts := []string{"terraform", command, "dir=" + path.Clean(dir)}
	return strings.Join(append(parts, args...), " ")
}

// shellSignature keys a shell line. A single simple command (one segment, no redirect, no
// background job, no substitution, no function definition) running kubectl, helm, terraform, docker,
// git or a cloud CLI keys like the dedicated tool, so a model that prefers shell hits the same
// fixtures (ruling R11).
func shellSignature(command string) string {
	parsed, err := shellwords.Split(command)
	if err == nil && len(parsed.Segments) == 1 && !parsed.Substitution && !parsed.FunctionDef {
		seg := parsed.Segments[0]
		if len(seg.Redirects) == 0 && !seg.Background && len(seg.Words) > 1 {
			w := seg.Words
			switch w[0] {
			case "kubectl":
				argv := w[1:]
				verbIdx := -1
				for i, arg := range argv {
					if resourceVerbs[arg] || arg == "logs" {
						verbIdx = i
						break
					}
				}
				if verbIdx > 0 {
					verb := argv[verbIdx]
					newArgv := []string{verb}
					newArgv = append(newArgv, argv[:verbIdx]...)
					newArgv = append(newArgv, argv[verbIdx+1:]...)
					return kubectlSignature(newArgv)
				}
				return kubectlSignature(argv)
			case "helm", "git", "docker", "aws", "az", "gcloud":
				return w[0] + " " + strings.Join(w[1:], " ")
			case "terraform":
				dir, rest := "", w[1:]
				if d, ok := strings.CutPrefix(rest[0], "-chdir="); ok {
					dir, rest = d, rest[1:]
				}
				if len(rest) > 0 {
					return terraformSignature(rest[0], dir, rest[1:])
				}
			}
		}
	}
	return "shell " + strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(command), ";")), " ")
}
