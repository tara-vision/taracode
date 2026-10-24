package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestShellRunsInTheWorkingDirectoryAndStreams(t *testing.T) {
	var stream bytes.Buffer
	tool := ShellTool(&stream)
	dir := t.TempDir()
	out, err := tool.Run(context.Background(), map[string]any{"command": "pwd && echo hi && echo err 1>&2"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, "hi") || !strings.Contains(out, "err") {
		t.Errorf("output %q", out)
	}
	if !strings.Contains(stream.String(), "hi") {
		t.Errorf("stream %q", stream.String())
	}
	out, err = tool.Run(context.Background(), map[string]any{"command": "exit 3"}, dir)
	if err != nil || !strings.Contains(out, "[exit status 3]") {
		t.Errorf("non-zero exit is reported in the text: %q %v", out, err)
	}
	_, err = tool.Run(context.Background(), map[string]any{"command": "sleep 5", "timeout": 1.0}, dir)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: %v", err)
	}
}

func TestShellClassifiesThroughTheShellClassifier(t *testing.T) {
	tool := ShellTool(nil)
	inv := tool.Classify(map[string]any{"command": "ls -la | head"}, "/w")
	if inv.Classification != policy.Read || inv.Command != "ls -la | head" {
		t.Errorf("%+v", inv)
	}
	inv = tool.Classify(map[string]any{"command": "curl https://api.example.com/x > out.json"}, "/w")
	if inv.Classification != policy.Mutate || !strings.Contains(inv.Reason, "redirect") {
		t.Errorf("%+v", inv)
	}
	inv = tool.Classify(map[string]any{"command": "dig api.example.com"}, "/w")
	if len(inv.Targets.Hosts) != 1 {
		t.Errorf("hosts %+v", inv.Targets)
	}
	if _, err := tool.Run(context.Background(), map[string]any{}, "/tmp"); err == nil {
		t.Error("command is required")
	}
}

// TestShellTimeoutStaysInItsBounds: a shell call's timeout is 60 seconds when it names none or a non-positive
// one, the one it names up to 600 seconds, and 600 seconds above that.
func TestShellTimeoutStaysInItsBounds(t *testing.T) {
	cases := []struct {
		args map[string]any
		want time.Duration
	}{
		{map[string]any{}, defaultShellTimeout},
		{map[string]any{"timeout": -5.0}, defaultShellTimeout},
		{map[string]any{"timeout": 30.0}, 30 * time.Second},
		{map[string]any{"timeout": 100000.0}, maxShellTimeout},
	}
	for _, c := range cases {
		if got := shellTimeout(c.args); got != c.want {
			t.Errorf("%v: %s, want %s", c.args, got, c.want)
		}
	}
}

// processExists reports whether a process whose command line carries fragment is running.
func processExists(t *testing.T, fragment string) bool {
	t.Helper()
	out, err := exec.Command("ps", "-A", "-o", "args=").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(out), fragment)
}

// waitGone fails the test unless every process whose command line carries fragment is gone within four seconds.
func waitGone(t *testing.T, fragment string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(t, fragment) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("a process with %q is still running", fragment)
}

// killMatching ends, at the end of the test, whatever process with fragment in its command line a failed
// assertion left behind.
func killMatching(t *testing.T, fragment string) {
	t.Helper()
	t.Cleanup(func() {
		out, _ := exec.Command("ps", "-A", "-o", "pid=,args=").Output()
		for _, line := range strings.Split(string(out), "\n") {
			if fields := strings.Fields(line); len(fields) > 1 && strings.Contains(line, fragment) {
				if pid, err := strconv.Atoi(fields[0]); err == nil {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		}
	})
}

// TestShellReturnsWhenABackgroundChildKeepsTheOutputOpen: a background process that inherits the command's output
// cannot hold the call past sh's exit: the call returns within the wait delay with sh's output and status, and the
// process is ended with the call, which the result says.
func TestShellReturnsWhenABackgroundChildKeepsTheOutputOpen(t *testing.T) {
	killMatching(t, "sleep 8.1")
	tool := ShellTool(nil)
	started := time.Now()
	out, err := tool.Run(context.Background(), map[string]any{"command": "sleep 8.1 & echo started"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the call took %s: the background child held it", took)
	}
	if !strings.Contains(out, "started") || !strings.Contains(out, leftBehindNote) {
		t.Errorf("output %q", out)
	}
	waitGone(t, "sleep 8.1")
}

// TestShellTimeoutKillsTheCommandsChildren: a timeout ends the command's children too, not sh alone.
func TestShellTimeoutKillsTheCommandsChildren(t *testing.T) {
	killMatching(t, "sleep 1234.5")
	tool := ShellTool(nil)
	_, err := tool.Run(context.Background(), map[string]any{"command": "sleep 1234.5; echo done", "timeout": 1.0}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	waitGone(t, "sleep 1234.5")
}

// TestShellEndsTheProcessesTheCommandLeavesBehind: a process the command backgrounds with its output redirected,
// which holds nothing of the call, is ended with the call all the same, and the result says so.
func TestShellEndsTheProcessesTheCommandLeavesBehind(t *testing.T) {
	killMatching(t, "sleep 1234.6")
	tool := ShellTool(nil)
	command := "(sleep 1234.6 >/dev/null 2>&1 &); echo leaving"
	out, err := tool.Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "leaving") || !strings.Contains(out, leftBehindNote) {
		t.Errorf("output %q", out)
	}
	waitGone(t, "sleep 1234.6")
}

// TestShellKillsALeftoverThatIgnoresTerm: a leftover that ignores TERM is killed after the grace.
func TestShellKillsALeftoverThatIgnoresTerm(t *testing.T) {
	killMatching(t, "sleep 1234.7")
	tool := ShellTool(nil)
	command := "trap '' TERM; sleep 1234.7 >/dev/null 2>&1 & echo leaving"
	out, err := tool.Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, leftBehindNote) {
		t.Errorf("output %q", out)
	}
	waitGone(t, "sleep 1234.7")
}

// TestShellReportsTheExitStatusOfACommandThatLeftAChildBehind: sh's exit status is reported even when a background
// child kept the output open, and it stays the result's last line (the evals read it there), the note before it.
func TestShellReportsTheExitStatusOfACommandThatLeftAChildBehind(t *testing.T) {
	killMatching(t, "sleep 8.2")
	tool := ShellTool(nil)
	out, err := tool.Run(context.Background(), map[string]any{"command": "sleep 8.2 & exit 3"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "[exit status 3]") || !strings.Contains(out, leftBehindNote) {
		t.Errorf("output %q", out)
	}
	waitGone(t, "sleep 8.2")
}

// TestShellKeepsAProcessDetachedWithSetsid: a process the command detaches with setsid leaves the call's process
// group and outlives the call; the result carries no note about it.
func TestShellKeepsAProcessDetachedWithSetsid(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("no setsid on this system")
	}
	killMatching(t, "sleep 1234.9")
	tool := ShellTool(nil)
	command := "setsid sleep 1234.9 >/dev/null 2>&1 </dev/null & echo detached"
	out, err := tool.Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "detached") || strings.Contains(out, leftBehindNote) {
		t.Errorf("output %q", out)
	}
	time.Sleep(200 * time.Millisecond)
	if !processExists(t, "sleep 1234.9") {
		t.Error("the detached process did not outlive the call")
	}
}

// TestShellTimeoutInterruptsFirstAndLetsTheCommandStop: the deadline interrupts the command's group, as Ctrl-C
// would, and a command that stops on the interrupt gets to say so before anything harsher.
func TestShellTimeoutInterruptsFirstAndLetsTheCommandStop(t *testing.T) {
	killMatching(t, "sleep 1234.4")
	tool := ShellTool(nil)
	command := "trap 'echo stopping; exit 0' INT; sleep 1234.4"
	started := time.Now()
	out, err := tool.Run(context.Background(), map[string]any{"command": command, "timeout": 1.0}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	if !strings.Contains(out, "stopping") {
		t.Errorf("the command was not interrupted first: %q", out)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the call took %s", took)
	}
	waitGone(t, "sleep 1234.4")
}

// TestShellForwardsTheOperatorsInterruptToTheCommandThenQuits: an interrupt taracode receives while a command runs
// goes to the command's group, the call waits for the group to stop, and only then does taracode quit on the
// signal (here recorded instead of acted on).
func TestShellForwardsTheOperatorsInterruptToTheCommandThenQuits(t *testing.T) {
	killMatching(t, "sleep 1234.2")
	var quitWith []os.Signal
	defer func(previous func(os.Signal)) { quit = previous }(quit)
	quit = func(sig os.Signal) { quitWith = append(quitWith, sig) }
	marker := filepath.Join(t.TempDir(), "started")
	go func() {
		for !fileExists(marker) {
			time.Sleep(20 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	tool := ShellTool(nil)
	command := "trap 'echo stopping; exit 0' INT; touch " + marker + "; sleep 1234.2"
	out, err := tool.Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "interrupt") {
		t.Fatalf("interrupted call: %v", err)
	}
	if !strings.Contains(out, "stopping") {
		t.Errorf("the command was not interrupted: %q", out)
	}
	if len(quitWith) != 1 || quitWith[0] != os.Interrupt {
		t.Errorf("quit with %v", quitWith)
	}
	waitGone(t, "sleep 1234.2")
}

// TestShellQuitsOnTheForwardedSignalForReal: run as a helper process, taracode's shell tool receives SIGINT while a
// command runs, and the process dies of that signal once the command has stopped (the real quit, not the recorder).
func TestShellQuitsOnTheForwardedSignalForReal(t *testing.T) {
	if os.Getenv("TARACODE_SHELL_HELPER") == "1" {
		command := "trap 'exit 0' INT; touch " + os.Getenv("TARACODE_SHELL_MARKER") + "; sleep 1234.1"
		_, _ = ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, os.TempDir())
		os.Exit(0) // not reached: the forwarded signal ends the process
	}
	killMatching(t, "sleep 1234.1")
	marker := filepath.Join(t.TempDir(), "started")
	helper := exec.Command(os.Args[0], "-test.run=TestShellQuitsOnTheForwardedSignalForReal")
	helper.Env = append(os.Environ(), "TARACODE_SHELL_HELPER=1", "TARACODE_SHELL_MARKER="+marker)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; !fileExists(marker); i++ {
		if i > 200 {
			_ = helper.Process.Kill()
			t.Fatal("the helper never started its command")
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = helper.Process.Signal(os.Interrupt)
	err := helper.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.Sys().(syscall.WaitStatus).Signal() != syscall.SIGINT {
		t.Errorf("the helper did not die of the interrupt: %v", err)
	}
	waitGone(t, "sleep 1234.1")
}

func TestDetachHintNamesSetsidOnlyWhereItExists(t *testing.T) {
	if hint := detachHint(true); !strings.Contains(hint, "setsid") || !strings.Contains(hint, ">FILE") {
		t.Errorf("%q", hint)
	}
	if hint := detachHint(false); !strings.Contains(hint, "no setsid") || !strings.Contains(hint, "service manager") {
		t.Errorf("%q", hint)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestShellCancelledWhileRunningInterruptsTheCommandAndReportsTheCancel: a context cancelled while the command runs
// interrupts the command's group and the call reports the cancellation, not a result.
func TestShellCancelledWhileRunningInterruptsTheCommandAndReportsTheCancel(t *testing.T) {
	killMatching(t, "sleep 1234.0")
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for !fileExists(marker) {
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	tool := ShellTool(nil)
	command := "trap 'echo stopping; exit 0' INT; touch " + marker + "; sleep 1234.0"
	out, err := tool.Run(ctx, map[string]any{"command": command}, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled call: %v (output %q)", err, out)
	}
	if !strings.Contains(out, "stopping") {
		t.Errorf("the command was not interrupted: %q", out)
	}
	waitGone(t, "sleep 1234.0")
}

// TestShellFailsToStartInAMissingDirectory: a working directory that does not exist is an error of the call, not a
// result.
func TestShellFailsToStartInAMissingDirectory(t *testing.T) {
	tool := ShellTool(nil)
	out, err := tool.Run(context.Background(), map[string]any{"command": "true"}, filepath.Join(t.TempDir(), "gone"))
	if err == nil || out != "" {
		t.Errorf("%q %v", out, err)
	}
}

// TestShellCannotOpenItsPipe: a system that refuses a pipe fails the call before anything runs.
func TestShellCannotOpenItsPipe(t *testing.T) {
	defer func(previous func() (*os.File, *os.File, error)) { newPipe = previous }(newPipe)
	newPipe = func() (*os.File, *os.File, error) { return nil, nil, errors.New("no pipe") }
	_, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": "true"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no pipe") {
		t.Errorf("%v", err)
	}
}

func TestExitStatusPassesAnUnexpectedRunErrorThrough(t *testing.T) {
	if code, err := exitStatus(nil); code != 0 || err != nil {
		t.Errorf("%d %v", code, err)
	}
	if _, err := exitStatus(errors.New("wait: boom")); err == nil || err.Error() != "wait: boom" {
		t.Errorf("%v", err)
	}
}

// TestShellReportsAFailureToWaitForTheCommand: a wait that fails for a reason other than the command's exit status
// is the call's error, output included.
func TestShellReportsAFailureToWaitForTheCommand(t *testing.T) {
	defer func(previous func(*exec.Cmd) error) { waitFor = previous }(waitFor)
	waitFor = func(cmd *exec.Cmd) error {
		_ = cmd.Wait()
		return errors.New("wait: boom")
	}
	out, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": "echo partial"}, t.TempDir())
	if err == nil || err.Error() != "wait: boom" || out != "partial" {
		t.Errorf("%q %v", out, err)
	}
}
