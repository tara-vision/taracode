package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

const cloudTimeout = 120 * time.Second

var cloudProviders = map[string]bool{"aws": true, "az": true, "gcloud": true}

// CloudTool runs the aws, az or gcloud CLI with an argument string.
func CloudTool() *Tool {
	return &Tool{
		Name: "cloud", ReadForm: true,
		Description: "Run a cloud CLI: provider aws, az or gcloud; args is everything after the binary, for example " +
			"\"ec2 describe-instances --region eu-west-1\".",
		Params: []Param{
			{Name: "provider", Type: "string", Description: "aws, az or gcloud",
				Enum: []string{"aws", "az", "gcloud"}, Required: true},
			{Name: "args", Type: "string", Description: "Arguments after the provider binary", Required: true},
		},
		Classify: func(args map[string]any, _ string) policy.Invocation {
			provider, raw := argString(args, "provider"), argString(args, "args")
			command := provider + " " + raw
			words, err := shellwords.Words(raw)
			if err != nil {
				return policy.Invocation{Tool: "cloud", Classification: policy.Mutate, Reason: err.Error(), Command: command}
			}
			res := classify.Cloud(provider, words)
			return policy.Invocation{Tool: "cloud", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: command, Targets: policy.Targets{CloudAccount: classify.CloudAccount(provider, words)}}
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			provider, err := required(args, "provider")
			if err != nil {
				return "", err
			}
			if !cloudProviders[provider] {
				return "", fmt.Errorf("provider must be aws, az or gcloud")
			}
			raw, err := required(args, "args")
			if err != nil {
				return "", err
			}
			words, err := shellwords.Words(raw)
			if err != nil {
				return "", err
			}
			ctx, cancel := withTimeout(ctx, cloudTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, provider, words...)
		},
	}
}
