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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The bounded shell call: the ladder a deadline or an operator's signal climbs on the command's group, the
// signals forwarded and quit on, the graces, and the draining of the output.

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

// detach is the command-line prefix that starts what follows it in a session of its own: setsid where the system
// has it, perl's POSIX::setsid elsewhere (macOS); the test is skipped with neither.
func detach(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("setsid"); err == nil {
		return "setsid"
	}
	if _, err := exec.LookPath("perl"); err == nil {
		return "perl -MPOSIX -e 'POSIX::setsid() or die; exec @ARGV' --"
	}
	t.Skip("nothing to detach with")
	return ""
}

// TestDrainClosesTheReaderWhenADetachedProcessHoldsTheOutput: a process the command detached into a session of its
// own still holds the write end; after drainGrace the reader is closed and the result keeps what was read.
func TestDrainClosesTheReaderWhenADetachedProcessHoldsTheOutput(t *testing.T) {
	killMatching(t, "sleep 20.4")
	defer func(previous time.Duration) { drainGrace = previous }(drainGrace)
	drainGrace = 300 * time.Millisecond
	command := "echo before; " + detach(t) + " sleep 20.4 & echo after"
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
	prefix := detach(t)
	killMatching(t, "sleep 20.6")
	defer func(previous time.Duration) { drainGrace = previous }(drainGrace)
	drainGrace = 1500 * time.Millisecond
	var quitWith []os.Signal
	defer func(previous func(os.Signal)) { quit = previous }(quit)
	quit = func(sig os.Signal) { quitWith = append(quitWith, sig) }
	caught := make(chan os.Signal, 1) // a signal that lands after the call has returned must not kill the binary
	signal.Notify(caught, os.Interrupt)
	defer signal.Stop(caught)
	sent := make(chan bool, 1)
	go func() {
		holder := false
		for i := 0; i < 100 && !holder; i++ {
			holder = processExists(t, "sleep 20.6") // the detached holder exists: sh is gone and the call drains
			time.Sleep(20 * time.Millisecond)
		}
		if holder {
			time.Sleep(200 * time.Millisecond)
			_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		}
		sent <- holder
	}()
	command := prefix + " sleep 20.6 & echo detached"
	_, err := ShellTool(nil).Run(context.Background(), map[string]any{"command": command}, t.TempDir())
	if !<-sent {
		t.Fatal("the detached holder never appeared")
	}
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
