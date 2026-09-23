package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

const kubectlTimeout = 120 * time.Second

// kubectlArgv builds the argv from the structured params plus the tokenized args. A structured
// namespace, context or output that is also spelled out in args (-n, --namespace, --context, -o,
// --output) is refused rather than silently picking one; a spelling this misses (-nkube-system)
// still gives classify.KubeTargets two different values, which it reports as "*".
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
				// The whole argv, not argv[1:]: a global flag passed as the verb (verb "-n", args
				// "kube-system delete ...") is part of the target, exactly as on the shell path.
				kubeContext, namespace := classify.KubeTargets(argv)
				inv.Targets = newKubeResolver(context.Background(), workingDir).targets(kubeContext, namespace,
					classify.KubeconfigFlag(argv), "")
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
