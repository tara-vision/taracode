package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

// TestKubectlArgvUndoesACommandRepeatedInArgs pins ruling P3-R59 (B4, B1): the shapes gemma4:12b sent
// in the first shake-out, the whole command line in args while the parameters are also set, become the
// argv of the command it meant, and a namespace, context or output repeated with the same value is
// merged. What is not a copy of a parameter stays as written.
func TestKubectlArgvUndoesACommandRepeatedInArgs(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"verb, resource and namespace repeated",
			map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "get pods -n billing"},
			"get pods -n billing"},
		{"the name the parameters lack stays",
			map[string]any{"verb": "get", "resource": "deployment", "namespace": "shop",
				"args": "get deployment checkout -n shop --context prod-cluster"},
			"get deployment -n shop checkout --context prod-cluster"},
		{"only the verb set", map[string]any{"verb": "get", "args": "get pod report-builder -n analytics"},
			"get pod report-builder -n analytics"},
		{"a leading kubectl", map[string]any{"verb": "get", "resource": "pods", "args": "kubectl get pods -o wide"},
			"get pods -o wide"},
		{"the resource by another alias", map[string]any{"verb": "get", "resource": "po", "args": "get pods"},
			"get po"},
		{"the plural of a resource the aliases do not know",
			map[string]any{"verb": "get", "resource": "certificate", "args": "get certificates -A"},
			"get certificate -A"},
		{"verb, resource and name repeated",
			map[string]any{"verb": "describe", "resource": "pod", "name": "x", "namespace": "shop",
				"args": "describe pod x -n shop"},
			"describe pod x -n shop"},
		{"resource and name repeated as type/name",
			map[string]any{"verb": "describe", "resource": "pod", "name": "x", "args": "describe pod/x"},
			"describe pod x"},
		{"type/name whose name the parameters lack",
			map[string]any{"verb": "describe", "resource": "pods", "args": "describe pod/x --show-events=false"},
			"describe pods x --show-events=false"},
		{"a type/name resource parameter repeated in two words",
			map[string]any{"verb": "describe", "resource": "deploy/web", "args": "describe deployment web"},
			"describe deploy/web"},
		{"the name of a verb with no resource",
			map[string]any{"verb": "logs", "name": "web-1", "args": "logs web-1 --tail=50"},
			"logs web-1 --tail=50"},
		{"the same namespace twice in args, in two spellings",
			map[string]any{"verb": "get", "resource": "pods", "namespace": "billing",
				"args": "-n billing --namespace=billing -l app=web"},
			"get pods -n billing -l app=web"},
		{"the same namespace, glued and = forms",
			map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "-nbilling -n=billing"},
			"get pods -n billing"},
		{"the same context", map[string]any{"verb": "get", "resource": "pods", "context": "kind-dev",
			"args": "get pods --context=kind-dev"}, "get pods --context kind-dev"},
		{"the same output in three spellings", map[string]any{"verb": "get", "resource": "pods", "output": "wide",
			"args": "-o wide --output=wide -owide"}, "get pods -o wide"},
		{"a namespace after -- belongs to the container's command",
			map[string]any{"verb": "exec", "name": "web", "namespace": "apps", "args": "-- env -n kube-system"},
			"exec web -n apps -- env -n kube-system"},
		{"a global flag as the verb is a command line of its own",
			map[string]any{"verb": "-n", "args": "kube-system delete pod x"}, "-n kube-system delete pod x"},
		{"events is a resource type, not another verb", map[string]any{"verb": "get", "args": "events -n shop"},
			"get events -n shop"},
		{"a config subcommand that shares a command's name",
			map[string]any{"verb": "config", "args": "set users.dev.token x"}, "config set users.dev.token x"},
		{"a lone kubectl word is a name", map[string]any{"verb": "logs", "args": "kubectl"}, "logs kubectl"},
		{"flags only, untouched", map[string]any{"verb": "get", "resource": "pods", "namespace": "apps",
			"output": "wide", "args": "-l app=web"}, "get pods -n apps -o wide -l app=web"},
	}
	for _, c := range cases {
		argv, err := KubectlArgv(c.params)
		if err != nil || strings.Join(argv, " ") != c.want {
			t.Errorf("%s: argv %q, err %v; want %q", c.name, argv, err, c.want)
		}
	}
}

// TestKubectlArgvRefusesWhatItCannotMerge: args that start with another kubectl verb, and a namespace,
// context or output given twice with different values, are argument errors whose text names what
// conflicts (ruling P3-R59).
func TestKubectlArgvRefusesWhatItCannotMerge(t *testing.T) {
	anotherVerb := `args starts with "describe" but verb is "get"; args holds only extra flags ` +
		`(for example -l app=web --tail=100), never the verb, resource, name, namespace or context`
	cases := []struct {
		name   string
		params map[string]any
		want   []string // substrings of the error
	}{
		{"another verb", map[string]any{"verb": "get", "args": "describe pod x"}, []string{anotherVerb}},
		{"another verb after kubectl", map[string]any{"verb": "get", "args": "kubectl describe pod x"},
			[]string{anotherVerb}},
		{"another verb before the repeated resource",
			map[string]any{"verb": "get", "resource": "pods", "args": "describe pods x"}, []string{anotherVerb}},
		{"a different namespace", map[string]any{"verb": "get", "resource": "pods", "namespace": "billing",
			"args": "get pods -n other"}, []string{"namespace", `"billing"`, `"other"`}},
		{"a different namespace, = form", map[string]any{"verb": "apply", "namespace": "sandbox",
			"args": "-f x.yaml --namespace=kube-system"}, []string{"namespace", `"sandbox"`, `"kube-system"`}},
		{"a different namespace, glued short form", map[string]any{"verb": "delete", "resource": "pod", "name": "x",
			"namespace": "apps", "args": "-nkube-system"}, []string{"namespace", `"apps"`, `"kube-system"`}},
		{"a different context", map[string]any{"verb": "apply", "context": "dev", "args": "-f x.yaml --context prod"},
			[]string{"context", `"dev"`, `"prod"`}},
		{"a different output", map[string]any{"verb": "get", "resource": "pods", "output": "json", "args": "-o yaml"},
			[]string{"output", `"json"`, `"yaml"`}},
		{"a namespace flag with no value", map[string]any{"verb": "get", "resource": "pods", "namespace": "billing",
			"args": "-n"}, []string{"namespace", `"billing"`, "-n", "no value"}},
		{"no verb", map[string]any{"args": "get pods"}, []string{"verb is required"}},
		{"an unbalanced quote", map[string]any{"verb": "get", "args": "-l 'app=web"}, nil},
	}
	for _, c := range cases {
		argv, err := KubectlArgv(c.params)
		if err == nil {
			t.Errorf("%s: argv %q, want an argument error", c.name, argv)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not say %q", c.name, err, w)
			}
		}
	}
}

// TestKubectlToolRunsTheNormalizedCommand drives the first shake-out shape through Run: kubectl sees
// the command once.
func TestKubectlToolRunsTheNormalizedCommand(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubectl)
	out, err := KubectlTool().Run(context.Background(),
		map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "get pods -n billing"}, "")
	if err != nil || out != "kubectl get pods -n billing" {
		t.Fatalf("%q %v", out, err)
	}
}

// TestKubectlClassifyKeepsTheVerbOfACallItRefuses pins ruling P3-R59 (B2): a call the tool refuses for
// its arguments keeps the verb parameter and its verb-level classification (a malformed scale is still
// a mutation, a malformed get a read), the command as given, and targets that name the error, so the
// gate's refusal and the audit record say what the call tried; nothing is resolved with kubectl.
func TestKubectlClassifyKeepsTheVerbOfACallItRefuses(t *testing.T) {
	fakeBin(t, "kubectl", "#!/bin/sh\necho \"kubectl ran $*\" >&2\nexit 1\n")
	tool := KubectlTool()
	scale := map[string]any{"verb": "scale", "resource": "deployment", "name": "checkout", "namespace": "shop",
		"context": "prod-cluster", "args": "scale deployment checkout --replicas=3 -n prod-shop"}
	reason := `namespace is given both as a parameter ("shop") and in args ("prod-shop") with different values; ` +
		`use one`
	want := policy.Invocation{Tool: "kubectl", Verb: "scale", Classification: policy.Mutate, Reason: reason,
		Command: "kubectl scale deployment checkout -n shop --context prod-cluster " +
			"scale deployment checkout --replicas=3 -n prod-shop",
		Targets: policy.Targets{KubeContext: "*", KubeNamespace: "*", KubeReason: reason}}
	if got := tool.Classify(scale, "/w"); got.Tool != want.Tool || got.Verb != want.Verb ||
		got.Classification != want.Classification || got.Reason != want.Reason || got.Command != want.Command ||
		got.Targets.KubeContext != "*" || got.Targets.KubeNamespace != "*" || got.Targets.KubeReason != reason {
		t.Errorf("classify\n got %+v\nwant %+v", got, want)
	}
	cases := []struct {
		params map[string]any
		verb   string
		class  policy.Classification
	}{
		{map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "-n other"}, "get", policy.Read},
		{map[string]any{"verb": "Delete", "args": "describe pod x"}, "delete", policy.Mutate},
		{map[string]any{"verb": "rollout", "namespace": "a", "args": "status deploy/x -n b"}, "rollout", policy.Mutate},
		{map[string]any{"args": "delete pod x"}, "", policy.Mutate},
	}
	for _, c := range cases {
		inv := tool.Classify(c.params, "/w")
		if inv.Verb != c.verb || inv.Classification != c.class || inv.Reason == "" || inv.Targets.KubeContext != "*" ||
			!strings.HasPrefix(inv.Command, "kubectl ") {
			t.Errorf("%v: %+v, want verb %q classified %s", c.params, inv, c.verb, c.class)
		}
	}
}

// TestRegistryArgumentErrorIsTheToolsOwnCheck: the gate asks the registry whether a call's arguments
// are ones the tool refuses; for kubectl that is KubectlArgv's error, redacted like a tool error, and
// no other tool refuses its arguments this way yet.
func TestRegistryArgumentErrorIsTheToolsOwnCheck(t *testing.T) {
	red, err := redact.New(redact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewBuiltinRegistry(Options{Redactor: red}, Config{})
	if err := r.ArgumentError("kubectl", map[string]any{"verb": "get", "args": "describe pod x"}); err == nil ||
		!strings.HasPrefix(err.Error(), `args starts with "describe" but verb is "get"`) {
		t.Errorf("another verb: %v", err)
	}
	fine := map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "get pods -n billing"}
	if err := r.ArgumentError("kubectl", fine); err != nil {
		t.Errorf("a normalized call is not an argument error: %v", err)
	}
	secret := map[string]any{"verb": "get", "namespace": "a", "args": "-n AKIAIOSFODNN7EXAMPLE"}
	if err := r.ArgumentError("kubectl", secret); err == nil || strings.Contains(err.Error(), "AKIAIOSFODNN7EXAMPLE") ||
		!strings.Contains(err.Error(), "[redacted:aws-access-key]") {
		t.Errorf("the reason must be redacted: %v", err)
	}
	if err := r.ArgumentError("helm", map[string]any{"args": "upgrade 'web"}); err != nil {
		t.Errorf("only kubectl refuses its arguments at the gate: %v", err)
	}
	if err := r.ArgumentError("no_such_tool", map[string]any{"verb": "get", "args": "describe pod x"}); err != nil {
		t.Errorf("an unknown tool has no argument check: %v", err)
	}
}

// TestKubectlArgvReadsParametersAsKubectlWould pins ruling P3-R64: a verb, resource or name parameter
// that holds several words runs as those words (kubectl refuses "pod x" as one argument), and a
// type,name word at the head of the command, which kubectl reads as two resource types and refuses,
// runs as type/name. A real list of resource types stays as written.
func TestKubectlArgvReadsParametersAsKubectlWould(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"a resource that holds the name",
			map[string]any{"verb": "describe", "resource": "pod invoice-worker-fbf95d7bd-ccb5n", "namespace": "billing"},
			"describe pod invoice-worker-fbf95d7bd-ccb5n -n billing"},
		{"a name that holds the resource",
			map[string]any{"verb": "describe", "name": "pod storefront-5996978c75-4pxx5", "namespace": "web"},
			"describe pod storefront-5996978c75-4pxx5 -n web"},
		{"a verb that holds the resource",
			map[string]any{"verb": "describe pod", "name": "orders-api-674454676b-dmf7d", "namespace": "api"},
			"describe pod orders-api-674454676b-dmf7d -n api"},
		{"a verb that holds its subcommand",
			map[string]any{"verb": "rollout status", "resource": "deployment/cart", "namespace": "shop-v2"},
			"rollout status deployment/cart -n shop-v2"},
		{"several words and the command repeated in args",
			map[string]any{"verb": "describe", "resource": "pod x", "namespace": "shop", "args": "describe pod x -n shop"},
			"describe pod x -n shop"},
		{"a global flag with its value as the verb", map[string]any{"verb": "-n kube-system", "args": "delete pod x"},
			"-n kube-system delete pod x"},
		{"type,name as the resource",
			map[string]any{"verb": "describe", "resource": "pod,metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
			"describe pod/metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"type,name as the name of logs",
			map[string]any{"verb": "logs", "name": "pod,metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
			"logs pod/metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"type,name of a deployment", map[string]any{"verb": "get", "resource": "deploy,metrics-agent", "namespace": "platform"},
			"get deploy/metrics-agent -n platform"},
		{"type,name at the head of args", map[string]any{"verb": "describe", "args": "describe pod,x -n platform"},
			"describe pod/x -n platform"},
		{"a list of resource types", map[string]any{"verb": "get", "resource": "pods,services", "namespace": "platform"},
			"get pods,services -n platform"},
		{"a list with a built-in type the aliases lack", map[string]any{"verb": "get", "resource": "pods,roles"},
			"get pods,roles"},
		{"a list of three", map[string]any{"verb": "get", "resource": "pod,a,b"}, "get pod,a,b"},
		{"a first part that is not a type", map[string]any{"verb": "get", "resource": "x,pod"}, "get x,pod"},
		{"a comma after a flag is not the head", map[string]any{"verb": "get", "args": "-n platform pod,x"},
			"get -n platform pod,x"},
	}
	for _, c := range cases {
		// Word by word: "pod x" as one argument is exactly what kubectl refuses.
		argv, err := KubectlArgv(c.params)
		if err != nil || !slices.Equal(argv, strings.Fields(c.want)) {
			t.Errorf("%s: argv %q, err %v; want %q", c.name, argv, err, strings.Fields(c.want))
		}
	}
}

// TestKubectlClassifiesTheWordsItRuns: the classifier reads the argv the tool runs, so a verb parameter
// that holds several words is classified by its first word, the way kubectl runs it.
func TestKubectlClassifiesTheWordsItRuns(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubectl)
	tool := KubectlTool()
	if inv := tool.Classify(map[string]any{"verb": "rollout status", "resource": "deployment/cart"}, "/w"); inv.Classification != policy.Read ||
		inv.Verb != "rollout status" || inv.Command != "kubectl rollout status deployment/cart" {
		t.Errorf("rollout status is a read: %+v", inv)
	}
	if inv := tool.Classify(map[string]any{"verb": "delete pod", "name": "x", "namespace": "kube-system"}, "/w"); inv.Classification != policy.Mutate ||
		inv.Verb != "delete" || inv.Targets.KubeNamespace != "kube-system" {
		t.Errorf("delete pod x is a mutation in kube-system: %+v", inv)
	}
}
