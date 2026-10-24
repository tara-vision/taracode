package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
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

// shortGraces shortens the interrupt and cancel graces for a test and restores them after it.
func shortGraces(t *testing.T, interrupt, cancelled time.Duration) {
	t.Helper()
	previous, previousCancel := interruptGrace, cancelGrace
	interruptGrace, cancelGrace = interrupt, cancelled
	t.Cleanup(func() { interruptGrace, cancelGrace = previous, previousCancel })
}

// TestShellTimeoutEndsACommandThatIgnoresTheInterrupt: a command that survives the interrupt gets the grace, then
// TERM, then KILL, so the timeout bounds the call.
func TestShellTimeoutEndsACommandThatIgnoresTheInterrupt(t *testing.T) {
	killMatching(t, "sleep 20.1")
	shortGraces(t, time.Second, time.Second)
	tool := ShellTool(nil)
	started := time.Now()
	_, err := tool.Run(context.Background(), map[string]any{"command": "trap '' INT TERM; sleep 20.1", "timeout": 1.0}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
	if took := time.Since(started); took > 8*time.Second {
		t.Errorf("the call took %s: the timeout did not bound it", took)
	}
	waitGone(t, "sleep 20.1")
}

// TestShellASecondInterruptEscalates: an operator's second signal does not wait for the grace: it moves the group
// to TERM at once, as a second Ctrl-C forces terraform's exit.
func TestShellASecondInterruptEscalates(t *testing.T) {
	killMatching(t, "sleep 20.2")
	defer func(previous func(os.Signal)) { quit = previous }(quit)
	quit = func(os.Signal) {}
	marker := filepath.Join(t.TempDir(), "started")
	go func() {
		for !fileExists(marker) {
			time.Sleep(20 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	started := time.Now()
	command := "trap '' INT; touch " + marker + "; sleep 20.2"
	_, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "interrupt") {
		t.Fatalf("interrupted call: %v", err)
	}
	if took := time.Since(started); took > 6*time.Second {
		t.Errorf("the call took %s: the second interrupt did not escalate", took)
	}
	waitGone(t, "sleep 20.2")
}

// TestShellACancelFromAboveEndsTheCommandQuickly: when the call's parent context ends it (an eval turn cancelled),
// the grace is the short one, so the caller gets its tool back well within its own wait.
func TestShellACancelFromAboveEndsTheCommandQuickly(t *testing.T) {
	killMatching(t, "sleep 20.3")
	shortGraces(t, 30*time.Second, 500*time.Millisecond)
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for !fileExists(marker) {
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	started := time.Now()
	command := "trap '' INT; touch " + marker + "; sleep 20.3"
	_, err := ShellTool(nil).Run(ctx, map[string]any{"command": command}, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled call: %v", err)
	}
	if took := time.Since(started); took > 8*time.Second {
		t.Errorf("the call took %s", took)
	}
	waitGone(t, "sleep 20.3")
}

// slowStream is a live stream that takes its time with every write, the way a lagging terminal does.
type slowStream struct {
	delay time.Duration
	mu    sync.Mutex
	buf   bytes.Buffer
}

func (s *slowStream) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *slowStream) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestShellKeepsTheWholeOutputWhenTheStreamIsSlow: the result carries every line the command wrote even when the
// live copy lags behind, and the live copy catches up before the call returns.
func TestShellKeepsTheWholeOutputWhenTheStreamIsSlow(t *testing.T) {
	stream := &slowStream{delay: 150 * time.Millisecond}
	tool := ShellTool(stream)
	command := "for i in 1 2 3 4 5 6; do echo line$i; sleep 0.05; done; echo END"
	out, err := tool.Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "END") || strings.Count(out, "line") != 6 {
		t.Errorf("the result lost output: %q", out)
	}
	if !strings.Contains(stream.String(), "END") {
		t.Errorf("the live copy did not catch up: %q", stream.String())
	}
}

// TestShellCommandsRunInTheirOwnSession: the command's session is its own (no controlling terminal), not taracode's.
func TestShellCommandsRunInTheirOwnSession(t *testing.T) {
	if os.Getenv("TARACODE_SHELL_SID_HELPER") == "1" {
		fmt.Println("sid", sessionID())
		os.Exit(0)
	}
	own := sessionID()
	command := "TARACODE_SHELL_SID_HELPER=1 " + os.Args[0] + " -test.run=TestShellCommandsRunInTheirOwnSession"
	out, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var sid int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "sid %d", &sid); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if sid == own || sid == 0 {
		t.Errorf("the command's session %d is taracode's %d", sid, own)
	}
}

// TestShellDescriptionIsTheSameOnEveryHost: the model-facing text names the rule, never the host's tools, so the
// two-machine board records one description.
func TestShellDescriptionIsTheSameOnEveryHost(t *testing.T) {
	d := ShellTool(nil).Description
	if strings.Contains(d, "setsid CMD") || strings.Contains(d, "cannot outlive") || !strings.Contains(d, "end with the call") {
		t.Errorf("%q", d)
	}
}

// TestShellDropsASignalTaracodeStartedWithIgnored: a signal ignored at start (a nohup, a background job of a
// script) is neither forwarded nor acted on: the command runs to its end and the call reports it normally.
func TestShellDropsASignalTaracodeStartedWithIgnored(t *testing.T) {
	ignoredAtStart[syscall.SIGHUP] = true
	defer delete(ignoredAtStart, syscall.SIGHUP)
	dir := t.TempDir()
	started, landed := filepath.Join(dir, "started"), filepath.Join(dir, "landed")
	go func() {
		for !fileExists(started) {
			time.Sleep(20 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
		time.Sleep(100 * time.Millisecond) // the signal is delivered before the command is allowed to end
		_ = os.WriteFile(landed, nil, 0o644)
	}()
	command := "touch " + started + "; while [ ! -e " + landed + " ]; do sleep 0.05; done; echo finished"
	out, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil || !strings.Contains(out, "finished") {
		t.Errorf("%q %v", out, err)
	}
}

// startSleeper starts `sleep` in a session of its own, the way the tool starts a command, for the unit tests of
// the ladder; it is ended at the end of the test.
func startSleeper(t *testing.T) *shellCall {
	t.Helper()
	call, err := startShell(context.Background(), "sleep 30", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-call.pgid, syscall.SIGKILL)
		_ = call.reader.Close()
		_, _ = call.cmd.Process.Wait()
	})
	return call
}

// TestNoteRecordsTheFirstSignalAndSendsAQuitStraightToKill: note keeps the first signal, drops an ignored one, and
// moves the ladder so that a SIGQUIT's next step is KILL.
func TestNoteRecordsTheFirstSignalAndSendsAQuitStraightToKill(t *testing.T) {
	call := &shellCall{}
	ignoredAtStart[syscall.SIGHUP] = true
	defer delete(ignoredAtStart, syscall.SIGHUP)
	if call.note(syscall.SIGHUP) || call.received != nil {
		t.Error("an ignored signal changed something")
	}
	if !call.note(os.Interrupt) || call.received != os.Interrupt || call.stage != 0 {
		t.Errorf("%+v", call)
	}
	if !call.note(syscall.SIGQUIT) || call.received != os.Interrupt || call.stage != 2 {
		t.Errorf("a quit did not move the ladder: %+v", call)
	}
}

// TestGoneReturnsAtOnceOnASignal: a forwarded signal cuts a wait for the group short, so the caller climbs the ladder.
func TestGoneReturnsAtOnceOnASignal(t *testing.T) {
	call := startSleeper(t)
	sigs := make(chan os.Signal, 1)
	sigs <- os.Interrupt
	started := time.Now()
	call.due = time.Now().Add(5 * time.Second)
	if call.gone(sigs) {
		t.Error("the group was reported gone while the sleep ran")
	}
	if time.Since(started) > time.Second || call.received != os.Interrupt {
		t.Errorf("the signal did not cut the wait short: %s %v", time.Since(started), call.received)
	}
}

// TestDrainClosesTheReaderWhenADetachedProcessHoldsTheOutput: a process the command detached into a session of its
// own still holds the write end; after drainGrace the reader is closed and the result keeps what was read.
func TestDrainClosesTheReaderWhenADetachedProcessHoldsTheOutput(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl to detach with")
	}
	killMatching(t, "sleep 20.4")
	defer func(previous time.Duration) { drainGrace = previous }(drainGrace)
	drainGrace = 300 * time.Millisecond
	command := "echo before; perl -MPOSIX -e 'POSIX::setsid() or die; exec @ARGV' -- sleep 20.4 & echo after"
	started := time.Now()
	out, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(started)
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") || took > 5*time.Second {
		t.Errorf("%q after %s", out, took)
	}
	if strings.Contains(out, leftBehindNote) || took < drainGrace {
		t.Errorf("the detached process was not what held the pipe: %q after %s", out, took)
	}
}

// TestDrainGivesTheLiveCopyBoundedTimeToCatchUp: a terminal more than drainGrace behind does not hold the call; the
// result is whole all the same.
func TestDrainGivesTheLiveCopyBoundedTimeToCatchUp(t *testing.T) {
	defer func(previous time.Duration) { drainGrace = previous }(drainGrace)
	drainGrace = 200 * time.Millisecond
	stream := &slowStream{delay: 600 * time.Millisecond}
	started := time.Now()
	out, err := ShellTool(stream).Run(context.Background(), map[string]any{"command": "echo one; echo two"}, t.TempDir())
	if err != nil || out != "one\ntwo" {
		t.Fatalf("%q %v", out, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Errorf("the slow terminal held the call for %s", time.Since(started))
	}
	if got := liveCopy(t, stream); got != "one\ntwo\n[live output cut; the result has it all]\n" {
		t.Errorf("the live copy %q", got)
	}
}

// TestTheCutNoteStartsOnItsOwnLine: a live copy cut in the middle of a line gets a newline before the note, one
// cut after a whole line does not (the test above).
func TestTheCutNoteStartsOnItsOwnLine(t *testing.T) {
	defer func(previous time.Duration) { drainGrace = previous }(drainGrace)
	drainGrace = 200 * time.Millisecond
	stream := &slowStream{delay: 600 * time.Millisecond}
	if _, err := ShellTool(stream).Run(context.Background(), map[string]any{"command": "printf one"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := liveCopy(t, stream); got != "one\n[live output cut; the result has it all]\n" {
		t.Errorf("the live copy %q", got)
	}
}

// liveCopy is the slow stream's content once its cut note has arrived (the streamer finishes after the call).
func liveCopy(t *testing.T, stream *slowStream) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if got := stream.String(); strings.Contains(got, "[live output cut") {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	return stream.String()
}

// TestShellActsOnASignalThatArrivesWhileDraining: an interrupt that lands after sh has exited, while the call
// still drains the output a detached process holds, is not lost: the call reports it and taracode quits.
func TestShellActsOnASignalThatArrivesWhileDraining(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl to detach with")
	}
	killMatching(t, "sleep 20.6")
	defer func(previous time.Duration) { drainGrace = previous }(drainGrace)
	drainGrace = 1500 * time.Millisecond
	var quitWith []os.Signal
	defer func(previous func(os.Signal)) { quit = previous }(quit)
	quit = func(sig os.Signal) { quitWith = append(quitWith, sig) }
	caught := make(chan os.Signal, 1) // a signal that lands after the call has returned must not kill the binary
	signal.Notify(caught, os.Interrupt)
	defer signal.Stop(caught)
	go func() {
		for i := 0; i < 100 && !processExists(t, "sleep 20.6"); i++ {
			time.Sleep(20 * time.Millisecond) // the detached holder exists: sh is gone and the call drains
		}
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	command := "perl -MPOSIX -e 'POSIX::setsid() or die; exec @ARGV' -- sleep 20.6 & echo detached"
	_, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "interrupt") || len(quitWith) != 1 {
		t.Errorf("the signal was lost: %v, quit %v", err, quitWith)
	}
}

// TestShellAParentCancelAfterTheOwnDeadlineShortensTheGrace: a caller that cancels while the command is already
// being interrupted does not wait the operator's grace; the short one applies from then on.
func TestShellAParentCancelAfterTheOwnDeadlineShortensTheGrace(t *testing.T) {
	killMatching(t, "sleep 20.7")
	shortGraces(t, 6*time.Second, 300*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(1500 * time.Millisecond) // the call's own 1 s deadline has passed: the ladder is at its interrupt
		cancel()
	}()
	started := time.Now()
	command := "trap '' INT; sleep 20.7"
	_, err := ShellTool(nil).Run(ctx, map[string]any{"command": command, "timeout": 1.0}, t.TempDir())
	if err == nil {
		t.Fatal("the call succeeded")
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the call took %s: the cancel did not shorten the grace", took)
	}
	waitGone(t, "sleep 20.7")
}

// TestShellClosesThePipeReaderAfterTheCall: a call leaves no file descriptor behind.
func TestShellClosesThePipeReaderAfterTheCall(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1)) // no finalizer may hide a leak
	tool := ShellTool(nil)
	before := openFds(t)
	for i := 0; i < 20; i++ {
		if _, err := tool.Run(context.Background(), map[string]any{"command": "echo hi"}, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	if after := openFds(t); after > before+2 {
		t.Errorf("open descriptors went from %d to %d over twenty calls", before, after)
	}
}

func openFds(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip("no /dev/fd here")
	}
	return len(entries)
}

// TestShortenCutsOnlyAGraceLongerThanTheCancelGrace: a command at its interrupt keeps a grace already shorter than
// cancelGrace, and a command at another stage is left alone.
func TestShortenCutsOnlyAGraceLongerThanTheCancelGrace(t *testing.T) {
	shortGraces(t, 30*time.Second, 5*time.Second)
	call := &shellCall{stage: 1, due: time.Now().Add(20 * time.Second)}
	if !call.shorten() || time.Until(call.due) > 5*time.Second {
		t.Errorf("a long grace was not cut: %s left", time.Until(call.due))
	}
	short := time.Now().Add(100 * time.Millisecond)
	call = &shellCall{stage: 1, due: short}
	if call.shorten() || call.due != short {
		t.Error("a grace already shorter than the cancel grace was changed")
	}
	call = &shellCall{stage: 2, due: time.Now().Add(20 * time.Second)}
	if call.shorten() {
		t.Error("a command past its interrupt was shortened")
	}
}

// TestNoteAllTakesEveryWaitingSignalWithoutBlocking: the signals waiting in the channel are noted, the first one
// kept as the one to quit on, and an empty channel returns at once.
func TestNoteAllTakesEveryWaitingSignalWithoutBlocking(t *testing.T) {
	sigs := make(chan os.Signal, 4)
	sigs <- syscall.SIGTERM
	sigs <- os.Interrupt
	call := &shellCall{}
	call.noteAll(sigs)
	if call.received != syscall.SIGTERM || len(sigs) != 0 {
		t.Errorf("received %v, %d left", call.received, len(sigs))
	}
	call.noteAll(sigs) // nothing waiting: returns at once
}

// TestGoneShortensTheGraceWhenTheParentEnds: a caller's context ending while the group is given its interrupt
// grace cuts that grace to the cancel grace.
func TestGoneShortensTheGraceWhenTheParentEnds(t *testing.T) {
	shortGraces(t, 30*time.Second, 300*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	call, err := startShell(ctx, "sleep 30", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-call.pgid, syscall.SIGKILL)
		_ = call.reader.Close()
		_, _ = call.cmd.Process.Wait()
	})
	call.stage, call.due = 1, time.Now().Add(20*time.Second)
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	if call.gone(make(chan os.Signal)) {
		t.Error("the group was reported gone while the sleep ran")
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("the parent's end did not shorten the grace: %s", took)
	}
}
