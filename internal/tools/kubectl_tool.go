package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

const (
	kubectlTimeout     = 120 * time.Second
	kubeResolveTimeout = 3 * time.Second
)

// currentKubeTarget asks kubectl for the current context and its default namespace; empty when
// kubectl is missing or has no context. Only mutate invocations pay this cost. kubectl config
// current-context/view read the kubeconfig (global or $KUBECONFIG), not project files, so a
// workingDir that does not exist (or is unset) must not fail the resolution the way it would fail
// an ordinary command run there; runCommand only sets cmd.Dir when dir is non-empty, so passing ""
// falls back to the process's own, always-valid, working directory.
func currentKubeTarget(ctx context.Context, workingDir string) (kubeContext, namespace string) {
	ctx, cancel := withTimeout(ctx, kubeResolveTimeout)
	defer cancel()
	dir := workingDir
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = ""
	}
	if out, err := runCommand(ctx, dir, "kubectl", "config", "current-context"); err == nil {
		kubeContext = strings.TrimSpace(out)
	}
	if out, err := runCommand(ctx, dir, "kubectl", "config", "view",
		"--minify", "-o", "jsonpath={..namespace}"); err == nil {
		namespace = strings.TrimSpace(out)
	}
	if namespace == "" || strings.HasSuffix(namespace, "completed with no output") {
		namespace = "default"
	}
	return kubeContext, namespace
}

// kubeTargetsFor fills the targets from the explicit flags, falling back to the current context.
func kubeTargetsFor(ctx context.Context, workingDir, explicitCtx, explicitNS string) policy.Targets {
	t := policy.Targets{KubeContext: explicitCtx, KubeNamespace: explicitNS}
	if t.KubeContext == "" || t.KubeNamespace == "" {
		cur, ns := currentKubeTarget(ctx, workingDir)
		if t.KubeContext == "" {
			t.KubeContext = cur
		}
		if t.KubeNamespace == "" {
			t.KubeNamespace = ns
		}
	}
	return t
}

// kubectlArgv builds the argv from the structured params plus the tokenized args. A structured
// namespace, context or output that is also spelled out in args (in any of its flag spellings) is
// refused rather than silently picking one: classify.KubeTargets and kubectl itself do not agree on
// which of two conflicting flags wins (KubeTargets reports the first match, kubectl applies the
// last), so target resolution could report a different namespace or context than the one kubectl
// actually mutates.
func kubectlArgv(args map[string]any) ([]string, error) {
	verb, err := required(args, "verb")
	if err != nil {
		return nil, err
	}
	extra, err := shellwords.Words(argString(args, "args"))
	if err != nil {
		return nil, err
	}
	argv := []string{verb}
	if r := argString(args, "resource"); r != "" {
		argv = append(argv, r)
	}
	if n := argString(args, "name"); n != "" {
		argv = append(argv, n)
	}
	if ns := argString(args, "namespace"); ns != "" {
		if hasWord(extra, "-n") || hasWord(extra, "--namespace") {
			return nil, fmt.Errorf("namespace is given both as a parameter and in args; use one")
		}
		argv = append(argv, "-n", ns)
	}
	if c := argString(args, "context"); c != "" {
		if hasWord(extra, "--context") {
			return nil, fmt.Errorf("context is given both as a parameter and in args; use one")
		}
		argv = append(argv, "--context", c)
	}
	if o := argString(args, "output"); o != "" {
		if hasWord(extra, "-o") || hasWord(extra, "--output") {
			return nil, fmt.Errorf("output is given both as a parameter and in args; use one")
		}
		argv = append(argv, "-o", o)
	}
	return append(argv, extra...), nil
}

// dropOutputFlag removes a "-o value" pair. kubectl diff, unlike apply, has no output-format flag;
// passing one through risks kubectl itself exiting 1 for an unrelated reason (an unrecognized flag),
// which the dry run's exit-status-1-means-differences check below cannot tell apart from real
// differences, so it must never reach the diff invocation.
func dropOutputFlag(argv []string) []string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-o" {
			i++ // also skip its value
			continue
		}
		out = append(out, argv[i])
	}
	return out
}

// KubectlTool runs kubectl from structured arguments.
func KubectlTool() *Tool {
	return &Tool{
		Name: "kubectl", ReadForm: true,
		Description: "Run kubectl. Read verbs (get, describe, logs, events, top, explain, diff, rollout status, " +
			"config view) work in investigate mode; apply, delete, patch, scale, exec need operate mode.",
		Params: []Param{
			{Name: "verb", Type: "string", Description: "kubectl verb, for example get, describe, logs, apply", Required: true},
			{Name: "resource", Type: "string", Description: "Resource type or type/name, for example pods or deploy/web"},
			{Name: "name", Type: "string", Description: "Resource name"},
			{Name: "namespace", Type: "string", Description: "Namespace (-n)"},
			{Name: "context", Type: "string", Description: "kubeconfig context (--context)"},
			{Name: "args", Type: "string", Description: "Extra flags, for example \"-l app=web --tail=100\""},
			{Name: "output", Type: "string", Description: "Output format (-o)", Enum: []string{"json", "yaml", "wide", "name"}},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			argv, err := kubectlArgv(args)
			if err != nil {
				return policy.Invocation{Tool: "kubectl", Classification: policy.Mutate, Reason: err.Error()}
			}
			res := classify.Kubectl(argv[0], argv[1:])
			inv := policy.Invocation{Tool: "kubectl", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: "kubectl " + strings.Join(argv, " ")}
			if res.Classification == policy.Mutate {
				explicitCtx, explicitNS := classify.KubeTargets(argv[1:])
				inv.Targets = kubeTargetsFor(context.Background(), workingDir, explicitCtx, explicitNS)
			}
			return inv
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			argv, err := kubectlArgv(args)
			if err != nil {
				return "", err
			}
			ctx, cancel := withTimeout(ctx, kubectlTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "kubectl", argv...)
		},
		DryRun: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			argv, err := kubectlArgv(args)
			if err != nil {
				return "", err
			}
			if argv[0] != "apply" {
				return "", ErrNoDryRun
			}
			ctx, cancel := withTimeout(ctx, kubectlTimeout)
			defer cancel()
			diffArgv := append([]string{"diff"}, dropOutputFlag(argv[1:])...)
			out, err := runCommand(ctx, workingDir, "kubectl", diffArgv...)
			if err != nil && strings.HasPrefix(err.Error(), "kubectl exited with status 1\n") {
				// kubectl diff exits 1 when there are differences; any other status is a real failure
				return strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[1]), nil
			}
			if err != nil {
				return out, err
			}
			if strings.HasSuffix(out, "completed with no output") {
				return "kubectl diff: no differences (the cluster already matches)", nil
			}
			return out, nil
		},
	}
}
