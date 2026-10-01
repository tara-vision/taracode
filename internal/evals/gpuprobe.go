package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tara-vision/taracode/internal/provider"
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
// --query-compute-apps=used_memory --format=csv,noheader,nounits). The command is the operator's own
// flag, never anything a model or an engine sent. It runs in its own process group, which is killed
// as a whole when the probe ends, when its time is up or when it prints too much. What it writes to
// stderr is dropped and what it writes to stdout is never repeated, so a warning about it cannot
// carry a host name or a path. It cannot prompt: it is not the terminal's foreground job.
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
	if cmd.Process != nil {
		killProcessGroup(cmd.Process.Pid) // a job the probe left in the background ends with it
	}
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil // the probe itself exited cleanly; only a job it left behind held its output open
	}
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
// Anything else on a line, no line at all, a sum of zero, or more memory than a GPU has is an error.
// The error names the line by its number and never repeats it: a probe's output is not ours to print.
func parseProbe(out string) (int64, error) {
	var total int64
	lines := 0
	for i, line := range strings.Split(out, "\n") {
		field := strings.TrimSpace(line)
		if field == "" {
			continue
		}
		lines++
		n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(field, "MiB")), 10, 64)
		if err != nil || n < 0 || n > probeMaxMiB {
			return 0, fmt.Errorf("the probe must print whole numbers of MiB, one per line: line %d is not one", i+1)
		}
		total += n
	}
	switch {
	case lines == 0:
		return 0, errors.New("the probe printed nothing")
	case total <= 0 || total > probeMaxMiB:
		return 0, fmt.Errorf("the probe's figures add up to %d MiB, which is not a GPU's memory in use", total)
	}
	return total, nil
}

// probeEnv is the environment the probe runs in: the one Run was started in, not the isolated one
// the tasks get. Outside Run there is no isolation, so it is the process's own.
func (o RunOptions) probeEnv() []string {
	if o.scope != nil {
		return o.scope.env
	}
	return os.Environ()
}

// measuredGPU is probeGPU for Run: the memory the GPU holds once the warm-up has loaded the model.
// It is measurement metadata, so anything that keeps it from being one costs a warning on the
// terminal and never the run:
//   - the engine did not describe the model as loaded (engine is nil): what the GPU holds cannot be
//     tied to the model, so the probe is not run;
//   - another model is on the GPU (others > 0): the GPU's figure would include it;
//   - the probe fails or prints something that is not a number of MiB;
//   - the figure is less than half of the engine's own estimate for the model, which is what a probe
//     that asks another machine, another GPU or prints another unit looks like.
//
// A run that is already cancelled measures nothing and says nothing.
func measuredGPU(ctx context.Context, opts RunOptions, engine *EngineInfo, others int) int64 {
	if opts.GPUProbe == "" || ctx.Err() != nil {
		return 0
	}
	warn := func(format string, args ...any) int64 {
		_, _ = fmt.Fprintf(opts.Out, "warning: no measured GPU memory in the results: "+format+"\n", args...)
		return 0
	}
	if engine == nil {
		return warn("the engine did not describe %s as loaded (see the warning above), so what the GPU holds "+
			"cannot be tied to it", opts.Model)
	}
	if others > 0 {
		noun, verb := "models", "are"
		if others == 1 {
			noun, verb = "model", "is"
		}
		return warn("%d other %s %s loaded on the engine and the GPU's figure would include it; "+
			"unload everything else and run again", others, noun, verb)
	}
	mib, err := probeGPU(ctx, opts.GPUProbe, opts.probeEnv())
	if err != nil {
		return warn("%v", err)
	}
	if mib*2 < engine.VRAMBytes/(1<<20) {
		return warn("the probe says %d MiB, less than half of the engine's own estimate of %d MiB; "+
			"check that it asks the engine's machine and GPU and prints MiB", mib, engine.VRAMBytes/(1<<20))
	}
	return mib
}

// unloadAfter asks the engine to drop the run's model when a probed run ends, so that the next run's
// probe finds nothing else on the GPU. Best effort, and on its own clock: the run's context may
// already be done.
func unloadAfter(ctx context.Context, opts RunOptions) {
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), engineCallTimeout)
	defer cancel()
	prov, err := provider.New(callCtx, opts.Host, opts.Vendor, opts.APIKey)
	if err != nil {
		return
	}
	_ = prov.LLM().Unload(callCtx, opts.Model)
}

// gib is a measured figure in the unit a card's size is given in.
func gib(mib int64) float64 { return float64(mib) / mibPerGiB }
