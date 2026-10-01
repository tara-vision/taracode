package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// probeTimeout bounds the operator's GPU probe command.
	probeTimeout = 15 * time.Second
	// probeOutputLimit is the most a probe may print: a few short lines of numbers.
	probeOutputLimit = 4096
	// probeMaxMiB is more memory than any GPU has; a larger figure is not a measurement.
	probeMaxMiB = 1 << 20
	// mibPerGiB converts the probe's unit to the one a card's size is given in.
	mibPerGiB = 1024
)

// probeOutput keeps what a probe prints, up to probeOutputLimit: past it the probe is stopped.
type probeOutput struct {
	buf     bytes.Buffer
	stop    context.CancelFunc
	flooded bool
}

func (p *probeOutput) Write(b []byte) (int, error) {
	if p.buf.Len()+len(b) > probeOutputLimit {
		p.flooded = true
		p.stop()
		return 0, errors.New("the probe printed too much")
	}
	return p.buf.Write(b)
}

// probeGPU runs the operator's probe command through the shell, in env, and returns the GPU memory
// in use in MiB. The engine's own figure for a loaded model is an estimate that can be far from what
// the GPU holds, so a board's memory column comes from the machine: the operator names a command
// that asks it (for an NVIDIA card on the engine's host, nvidia-smi with
// --query-gpu=memory.used --format=csv,noheader,nounits). The command is the operator's own flag,
// never anything a model or an engine sent. It runs in its own process group, which is killed as a
// whole when the probe's time is up or it prints too much, and what it writes to stderr is dropped,
// so a warning about it never repeats a host name or a path.
func probeGPU(ctx context.Context, command string, env []string) (int64, error) {
	callCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := &probeOutput{stop: cancel}
	cmd := exec.CommandContext(callCtx, "sh", "-c", command)
	cmd.Env, cmd.Stdout = env, out
	cmd.WaitDelay = scriptWaitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	err := cmd.Run()
	switch {
	case out.flooded:
		return 0, errors.New("the probe printed too much: it must print the memory in use in MiB and nothing else")
	case err != nil && callCtx.Err() != nil:
		return 0, errors.New("the probe did not finish in time")
	case err != nil:
		return 0, fmt.Errorf("the probe failed: %w", err)
	}
	return parseProbe(out.buf.String())
}

// parseProbe adds up what a probe printed: one whole number of MiB per line, with or without the
// unit, blank lines skipped. One line per GPU process is as good as one line for the whole GPU.
// Anything else on a line, a sum of zero, or more memory than a GPU has is an error.
func parseProbe(out string) (int64, error) {
	var total int64
	for _, line := range strings.Split(out, "\n") {
		field := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "MiB"))
		if field == "" {
			continue
		}
		n, err := strconv.ParseInt(field, 10, 64)
		if err != nil || n < 0 || n > probeMaxMiB {
			return 0, fmt.Errorf("the probe must print whole numbers of MiB, it printed %q", clip(strings.TrimSpace(line), 40))
		}
		total += n
	}
	if total <= 0 || total > probeMaxMiB {
		return 0, fmt.Errorf("the probe's figures add up to %d MiB, which is not a GPU's memory in use", total)
	}
	return total, nil
}

// clip is s cut to n characters, so a warning never repeats a long line.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// measuredGPU is probeGPU for Run: the memory the GPU holds once the warm-up has loaded the model.
// It is measurement metadata, so a failure is one warning on the terminal and never the run's. The
// probe runs in the environment Run was started in, not the isolated one the tasks get.
func measuredGPU(ctx context.Context, opts RunOptions) int64 {
	if opts.GPUProbe == "" {
		return 0
	}
	mib, err := probeGPU(ctx, opts.GPUProbe, opts.scope.env)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Out, "warning: no measured GPU memory in the results: %v\n", err)
		return 0
	}
	return mib
}

// gib is a measured figure in the unit a card's size is given in.
func gib(mib int64) float64 { return float64(mib) / mibPerGiB }
