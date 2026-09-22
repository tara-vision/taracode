package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
)

const (
	defaultShellTimeout = 60 * time.Second
	maxShellTimeout     = 600 * time.Second
)

// withTimeout derives a per-call context; a zero d keeps the parent's deadline.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// ShellTool runs a command line with sh -c in the working directory. stream, when set, receives the
// output live; the result carries the merged output and the exit status.
func ShellTool(stream io.Writer) *Tool {
	return &Tool{
		Name: "shell", ReadForm: true,
		Description: "Run a shell command in the working directory. Read-only commands (cat, grep, ps, df, dig, jq, " +
			"kubectl get, git status, ...) work in investigate mode; anything else needs operate mode.",
		Params: []Param{
			{Name: "command", Type: "string", Description: "The command line", Required: true},
			{Name: "timeout", Type: "integer", Description: "Seconds before the command is killed (default 60, max 600)"},
		},
		Classify: func(args map[string]any, _ string) policy.Invocation {
			command := argString(args, "command")
			res := classify.Shell(command)
			return policy.Invocation{Tool: "shell", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: command, Targets: policy.Targets{Hosts: res.Hosts}}
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			command, err := required(args, "command")
			if err != nil {
				return "", err
			}
			timeout := time.Duration(argInt(args, "timeout", 0)) * time.Second
			if timeout <= 0 {
				timeout = defaultShellTimeout
			}
			if timeout > maxShellTimeout {
				timeout = maxShellTimeout
			}
			ctx, cancel := withTimeout(ctx, timeout)
			defer cancel()
			//nolint:gosec // the shell tool runs what the model asked for, after the policy gate
			cmd := exec.CommandContext(ctx, "sh", "-c", command)
			cmd.Dir = workingDir
			var buf bytes.Buffer
			var w io.Writer = &buf
			if stream != nil {
				w = io.MultiWriter(&buf, stream)
			}
			cmd.Stdout, cmd.Stderr = w, w
			runErr := cmd.Run()
			out := strings.TrimRight(buf.String(), "\n")
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return out, fmt.Errorf("command timed out after %s", timeout)
			}
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				return out + fmt.Sprintf("\n[exit status %d]", exitErr.ExitCode()), nil
			}
			if runErr != nil {
				return out, runErr
			}
			if out == "" {
				return "(no output)", nil
			}
			return out, nil
		},
	}
}
