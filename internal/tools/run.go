package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// waitDelay bounds the wait for a command's output once the command has exited or its context has
// ended: a child it started that keeps the output open cannot hold the call past its timeout.
const waitDelay = 2 * time.Second

// runCommand runs name with args in dir under ctx and returns stdout and stderr together. A non-zero
// exit is an error whose text carries the output, so the model sees why the command failed; a
// deadline is reported as a timeout.
func runCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the tool layer runs the CLIs the user asked for
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.WaitDelay = waitDelay
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := strings.TrimRight(out.String(), "\n")
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return text, fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return text, fmt.Errorf("%s exited with status %d\n%s", name, exitErr.ExitCode(), text)
		}
		return text, fmt.Errorf("%s: %w", name, err)
	}
	if text == "" {
		return name + " " + strings.Join(args, " ") + " completed with no output", nil
	}
	return text, nil
}

// runOutput runs name with args in dir under ctx, with env (NAME=value entries) added to the process
// environment, and returns its standard output alone: a warning on standard error never becomes
// part of the value read. Any failure is an error: a non-zero exit, a timeout, or an output a child
// kept open past waitDelay.
func runOutput(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // kubectl config reads, with fixed arguments
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.WaitDelay = waitDelay
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return strings.TrimSpace(out.String()), nil
}
