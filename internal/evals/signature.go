package evals

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/tara-vision/taracode/internal/tools"
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
		return cloudSignature(str(args, "provider"), words(str(args, "args")))
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

// cloudProviders are the CLIs a shell line can alias to a cloud call (ruling R11, spec 5.3); the
// dedicated tool otherwise accepts any provider string.
var cloudProviders = map[string]bool{"aws": true, "az": true, "gcloud": true}

// cloudSignature keys aws, az and gcloud as the shell alias does ("<provider> <args>"), matching
// shellSignature's own case list; any other provider is keyed "cloud <provider> <args>" so it can
// never equal another tool's own signature (ruling P3-R38) - without this, {"provider": "terraform",
// "args": "plan dir=."} would render as "terraform plan dir=.", the real tool's own plan signature,
// and unlock its apply gate without a real terraform call ever having happened.
func cloudSignature(provider string, args []string) string {
	if cloudProviders[provider] {
		return strings.TrimSpace(provider + " " + strings.Join(args, " "))
	}
	return strings.TrimSpace("cloud " + provider + " " + strings.Join(args, " "))
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

// kubectlArgv is the argv the kubectl tool runs for its structured parameters, from tools.KubectlArgv
// itself, so the dedicated tool and a shell line meet in kubectlSignature and a command line a model
// repeats in args keys the command the tool would run (ruling P3-R59). A call the tool refuses for its
// arguments never runs or replays, since the gate refuses it first; it keys its parameters as given,
// so the notes and the calls log still name what it tried.
func kubectlArgv(args map[string]any) []string {
	if argv, err := tools.KubectlArgv(args); err == nil {
		return argv
	}
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

// canonicalResource names a resource type the way the kubectl tool compares one
// (tools.CanonicalKubeResource), so the signature and the tool agree on what one type is.
func canonicalResource(r string) string {
	return tools.CanonicalKubeResource(r)
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
	parts = appendKubectlResourceAndName(parts, verb, positional)
	parts = appendKubectlFields(parts, fields)
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

// appendKubectlResourceAndName adds resource and name to parts and returns the extended slice.
func appendKubectlResourceAndName(parts []string, verb string, positional []string) []string {
	if len(positional) == 0 {
		return parts
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
	parts = append(parts, item)
	return append(parts, rest...)
}

// appendKubectlFields adds flags and their values to parts and returns the extended slice.
func appendKubectlFields(parts []string, fields map[string]string) []string {
	for _, f := range []struct {
		key  string
		flag string
	}{{"namespace", "-n"}, {"context", "--context"}, {"output", "-o"}, {"kubeconfig", "--kubeconfig"}} {
		if v := fields[f.key]; v != "" {
			parts = append(parts, f.flag, v)
		}
	}
	return parts
}

// findKubectlVerb finds the kubectl verb in argv, properly skipping flags and their values.
// Returns the verb and argv reordered with verb first, or empty verb if none found.
func findKubectlVerb(argv []string) (verb string, reordered []string) {
	for i := 0; i < len(argv); i++ {
		t := argv[i]

		// Flag with = value (-n=value or --namespace=value)
		if name, _, ok := strings.Cut(t, "="); ok && kubectlValueFlags[name] != "" {
			continue
		}

		// Flag with separate next value (-n value or --namespace value)
		if kubectlValueFlags[t] != "" && i+1 < len(argv) {
			i++ // skip both flag and value
			continue
		}

		// Short flag with attached value (-nshop or -owide)
		if (strings.HasPrefix(t, "-n") || strings.HasPrefix(t, "-o")) && len(t) > 2 && !strings.HasPrefix(t, "--") {
			continue
		}

		// Any other flag
		if strings.HasPrefix(t, "-") {
			continue
		}

		// Found the verb
		verb = t
		reordered = make([]string, 0, len(argv))
		reordered = append(reordered, verb)
		reordered = append(reordered, argv[:i]...)
		reordered = append(reordered, argv[i+1:]...)
		return
	}

	return "", argv
}

// terraformSignature renders "terraform <command> dir=<clean dir> [<args as written>]". Since the
// whole signature is space-joined and later split with strings.Fields (terraformSig in replay.go), a
// dir containing whitespace is quoted with strconv.Quote so two directories that merely share a
// leading word ("my infra" and "my other") key differently instead of both truncating to "my"
// (ruling P3-R38).
func terraformSignature(command, dir string, args []string) string {
	if dir == "" {
		dir = "."
	}
	dir = path.Clean(dir)
	dirField := dir
	if len(strings.Fields(dir)) > 1 {
		dirField = strconv.Quote(dir)
	}
	parts := []string{"terraform", command, "dir=" + dirField}
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
				verb, reordered := findKubectlVerb(argv)
				if verb != "" {
					return kubectlSignature(reordered)
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
