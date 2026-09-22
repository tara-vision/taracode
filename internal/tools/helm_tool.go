package tools

import (
	"context"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

const helmTimeout = 300 * time.Second

func helmWords(args map[string]any) ([]string, string, error) {
	raw, err := required(args, "args")
	if err != nil {
		return nil, "", err
	}
	words, err := shellwords.Words(raw)
	return words, raw, err
}

// HelmTool runs helm with an argument string; install and upgrade have a --dry-run dry run.
func HelmTool() *Tool {
	return &Tool{
		Name: "helm", ReadForm: true,
		Description: "Run helm. args is everything after \"helm\", for example \"list -A\" or " +
			"\"upgrade web ./chart -n apps\".",
		Params: []Param{{Name: "args", Type: "string", Description: "Arguments after helm", Required: true}},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			words, raw, err := helmWords(args)
			if err != nil {
				return policy.Invocation{Tool: "helm", Classification: policy.Mutate, Reason: err.Error(),
					Command: "helm " + raw}
			}
			res := classify.Helm(words)
			inv := policy.Invocation{Tool: "helm", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: "helm " + raw}
			if res.Classification == policy.Mutate {
				_, ns := classify.KubeTargets(words)
				inv.Targets = kubeTargetsFor(context.Background(), workingDir, helmContext(words), ns)
			}
			return inv
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			words, _, err := helmWords(args)
			if err != nil {
				return "", err
			}
			ctx, cancel := withTimeout(ctx, helmTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "helm", words...)
		},
		DryRun: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			words, _, err := helmWords(args)
			if err != nil {
				return "", err
			}
			if len(words) == 0 || (words[0] != "upgrade" && words[0] != "install") {
				return "", ErrNoDryRun
			}
			if !hasWord(words, "--dry-run") {
				words = append(words, "--dry-run")
			}
			ctx, cancel := withTimeout(ctx, helmTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "helm", words...)
		},
	}
}

func helmContext(words []string) string {
	for i, w := range words {
		if strings.HasPrefix(w, "--kube-context=") {
			return strings.TrimPrefix(w, "--kube-context=")
		}
		if w == "--kube-context" && i+1 < len(words) {
			return words[i+1]
		}
	}
	return ""
}

func hasWord(words []string, want string) bool {
	for _, w := range words {
		if w == want || strings.HasPrefix(w, want+"=") {
			return true
		}
	}
	return false
}
