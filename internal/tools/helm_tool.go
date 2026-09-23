package tools

import (
	"context"
	"errors"
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
				kubeContext, ns := helmEnvTargets(classify.HelmTargets(words))
				inv.Targets = newKubeResolver(context.Background(), workingDir).targets(kubeContext, ns,
					classify.KubeconfigFlag(words))
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
			if hasWord(words, "--post-renderer") {
				return "", errPostRendererDryRun
			}
			ctx, cancel := withTimeout(ctx, helmTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "helm", helmDryRunArgv(words)...)
		},
	}
}

// errPostRendererDryRun refuses the dry run of a release with a post-renderer: helm runs the
// --post-renderer program while rendering, so the dry run itself would run it before the user
// approves the call. No other helm option runs a program.
var errPostRendererDryRun = errors.New("the dry run would run the --post-renderer program before you approve " +
	"the call; run the upgrade or install without --post-renderer")

// helmDryRunArgv makes the invocation a real dry run: a --dry-run the model wrote is dropped (with
// =false or =none helm would install or upgrade for real) and one --dry-run goes at the end of the
// flags, before a "--" after which it would be a release name.
func helmDryRunArgv(words []string) []string {
	out := make([]string, 0, len(words)+1)
	for i, w := range words {
		if w == "--" {
			return append(append(out, "--dry-run"), words[i:]...)
		}
		if w != "--dry-run" && !strings.HasPrefix(w, "--dry-run=") {
			out = append(out, w)
		}
	}
	return append(out, "--dry-run")
}

func hasWord(words []string, want string) bool {
	for _, w := range words {
		if w == want || strings.HasPrefix(w, want+"=") {
			return true
		}
	}
	return false
}
