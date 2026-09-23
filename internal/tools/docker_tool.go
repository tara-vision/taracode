package tools

import (
	"context"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

const dockerTimeout = 300 * time.Second

// DockerTool runs docker with an argument string.
func DockerTool() *Tool {
	return &Tool{
		Name: "docker", ReadForm: true,
		Description: "Run docker. args is everything after \"docker\", for example \"ps -a\", \"logs web --tail 100\" " +
			"or \"compose ps\".",
		Params: []Param{{Name: "args", Type: "string", Description: "Arguments after docker", Required: true}},
		Classify: func(args map[string]any, _ string) policy.Invocation {
			raw := argString(args, "args")
			words, err := shellwords.Words(raw)
			if err != nil {
				return policy.Invocation{Tool: "docker", Classification: policy.Mutate, Reason: err.Error(),
					Command: "docker " + raw}
			}
			res := classify.Docker(words)
			return policy.Invocation{Tool: "docker", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: "docker " + raw}
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
			// The verb after any global flags: "docker --context x stats" streams too, so add
			// --no-stream whenever the verb is stats, or it runs until the command timeout.
			if classify.Docker(words).Verb == "stats" && !hasWord(words, "--no-stream") {
				words = append(words, "--no-stream")
			}
			ctx, cancel := withTimeout(ctx, dockerTimeout)
			defer cancel()
			return runCommand(ctx, workingDir, "docker", words...)
		},
	}
}
