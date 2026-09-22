package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runCommand runs name with args in dir under ctx and returns stdout and stderr together. A non-zero
// exit is an error whose text carries the output, so the model sees why the command failed; a
// deadline is reported as a timeout.
func runCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the tool layer runs the CLIs the user asked for
	if dir != "" {
		cmd.Dir = dir
	}
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
