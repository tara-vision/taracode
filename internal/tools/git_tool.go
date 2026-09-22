package tools

import (
	"context"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

const gitTimeout = 120 * time.Second

// GitTool runs git with an argument string in the working directory.
func GitTool() *Tool {
	return &Tool{
		Name: "git", ReadForm: true,
		Description: "Run git in the working directory. args is everything after \"git\", for example " +
			"\"log --oneline -n 10\".",
		Params: []Param{{Name: "args", Type: "string", Description: "Arguments after git", Required: true}},
		Classify: func(args map[string]any, _ string) policy.Invocation {
			raw := argString(args, "args")
			words, err := shellwords.Words(raw)
			if err != nil {
				return policy.Invocation{
					Tool: "git", Classification: policy.Mutate,
					Reason: "arguments could not be parsed: " + err.Error(), Command: "git " + raw,
				}
			}
			res := classify.Git(words)
			return policy.Invocation{Tool: "git", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: "git " + raw}
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			raw, err := required(args, "args")
			if err != nil {
				return "", err
			}
			words, err := shellwords.Words(raw)
			if err != nil {
				return "", err
			}
			ctx, cancel := withTimeout(ctx, gitTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "git", words...)
		},
	}
}
