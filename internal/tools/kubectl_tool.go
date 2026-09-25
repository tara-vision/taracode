package tools

import (
	"context"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
)

const kubectlTimeout = 120 * time.Second

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
			{Name: "args", Type: "string", Description: "Only extra flags, for example \"-l app=web --tail=100\"; " +
				"never the verb, resource, name, namespace or context"},
			{Name: "output", Type: "string", Description: "Output format (-o)", Enum: []string{"json", "yaml", "wide", "name"}},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			argv, err := KubectlArgv(args)
			if err != nil {
				return refusedKubectlInvocation(args, err)
			}
			res := classify.Kubectl(argv[0], argv[1:])
			inv := policy.Invocation{Tool: "kubectl", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: "kubectl " + strings.Join(argv, " ")}
			if res.Classification == policy.Mutate {
				// The whole argv, not argv[1:]: a global flag passed as the verb (verb "-n", args
				// "kube-system delete ...") is part of the target, exactly as on the shell path. The cause
				// of a "*" namespace the namespace objects make goes with it, so the deny names its remedy.
				kubeContext, namespace, cause := classify.KubeTargetsWithCause(argv)
				inv.Targets = newKubeResolver(context.Background(), workingDir).targets(kubeContext, namespace,
					classify.KubeconfigFlag(argv), cause)
			}
			return inv
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			argv, err := KubectlArgv(args)
			if err != nil {
				return "", err
			}
			ctx, cancel := withTimeout(ctx, kubectlTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "kubectl", argv...)
		},
		DryRun: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			argv, err := KubectlArgv(args)
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

// refusedKubectlInvocation classifies a call whose arguments the tool refuses (ruling P3-R59): the verb
// parameter with its verb-level classification, so a malformed scale is still a mutation and a
// malformed get a read, the command as the parameters spell it, and "*" targets that carry the error.
// The gate refuses such a call before the mode or the policy sees it (Registry.ArgumentError); the "*"
// targets keep the policy's protected lists in force for anything that evaluates it anyway.
func refusedKubectlInvocation(args map[string]any, err error) policy.Invocation {
	verb := argString(args, "verb")
	return policy.Invocation{Tool: "kubectl", Verb: strings.ToLower(verb),
		Classification: classify.Kubectl(verb, nil).Classification, Reason: err.Error(),
		Command: kubectlCommandAsGiven(args),
		Targets: policy.Targets{KubeContext: "*", KubeNamespace: "*", KubeReason: err.Error()}}
}

// kubectlCommandAsGiven is the command line the parameters spell before any normalization; it feeds
// the audit record. ArgumentError refuses the call before the policy ever sees the invocation, so no
// deny pattern reads it.
func kubectlCommandAsGiven(args map[string]any) string {
	parts := []string{"kubectl"}
	for _, p := range []string{"verb", "resource", "name"} {
		if v := argString(args, p); v != "" {
			parts = append(parts, v)
		}
	}
	for _, f := range kubectlFlagParams {
		if v := argString(args, f.param); v != "" {
			parts = append(parts, f.flag(), v)
		}
	}
	if extra := argString(args, "args"); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}
