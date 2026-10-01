package evals

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// stubHost replaces the host name and the CNAME lookup the default deny list is built from, and
// unsets the variable that would replace that list.
func stubHost(t *testing.T, name string, nameErr error, cname string, cnameErr error) {
	t.Helper()
	t.Setenv(privateNamesEnvVar, "")
	if err := os.Unsetenv(privateNamesEnvVar); err != nil {
		t.Fatal(err)
	}
	oldHost, oldCNAME := lookupHostname, lookupCNAME
	lookupHostname = func() (string, error) { return name, nameErr }
	lookupCNAME = func(context.Context, string) (string, error) { return cname, cnameErr }
	t.Cleanup(func() { lookupHostname, lookupCNAME = oldHost, oldCNAME })
}

// machineNames is what the default list adds after the host's own names on this machine.
func machineNames(short string) []string {
	var names []string
	domains := resolvSearchDomains()
	for _, d := range domains {
		names = append(names, short+"."+d)
	}
	return append(append(names, domains...), physicalInterfaceAddrs()...)
}

func TestDefaultPrivateNamesCollectTheHostsNames(t *testing.T) {
	stubHost(t, "kiosk-7.local", nil, "kiosk-7.corp.example.", nil)
	names, err := privateNames()
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"kiosk-7.local", "kiosk-7", "kiosk-7.corp.example"}, machineNames("kiosk-7.local")...)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names %q\nwant  %q", names, want)
	}
}

// TestDefaultPrivateNamesSkipAnUnhelpfulCNAME: a CNAME that is the short name again, or a failed
// lookup, adds nothing.
func TestDefaultPrivateNamesSkipAnUnhelpfulCNAME(t *testing.T) {
	for _, tt := range []struct {
		cname string
		err   error
	}{{"KIOSK-7.", nil}, {"", errors.New("no such host")}} {
		stubHost(t, "kiosk-7", nil, tt.cname, tt.err)
		names, err := defaultPrivateNames()
		want := append([]string{"kiosk-7"}, machineNames("kiosk-7")...)
		if err != nil || !reflect.DeepEqual(names, want) {
			t.Errorf("cname %q: %q %v", tt.cname, names, err)
		}
	}
}

func TestRecordTaskNeedsTheHostName(t *testing.T) {
	task, scenarios, _ := newRecordingTask(t, "unnamed-host", "- {tool: kubectl, args: {verb: get, resource: pods}}")
	stubHost(t, "", errors.New("uname failed"), "", nil)
	err := RecordTask(context.Background(), task, scenarios, nil, io.Discard)
	if err == nil || err.Error() != "crashloop-oomkilled: look up the host name: uname failed" {
		t.Fatalf("err = %v", err)
	}
}

func TestIsVirtualInterfaceName(t *testing.T) {
	for name, want := range map[string]bool{"lo": true, "docker0": true, "br-1a2b": true, "veth9f": true, "virbr0": true,
		"lo0": false, "en0": false, "eth0": false, "wlan0": false} {
		if got := isVirtualInterfaceName(name); got != want {
			t.Errorf("isVirtualInterfaceName(%q) = %v", name, got)
		}
	}
}

// TestPhysicalInterfaceAddrsAreGlobalUnicast holds on any machine: what comes back is a global
// unicast address, never a loopback or link-local one.
func TestPhysicalInterfaceAddrsAreGlobalUnicast(t *testing.T) {
	for _, a := range physicalInterfaceAddrs() {
		if ip := net.ParseIP(a); ip == nil || !ip.IsGlobalUnicast() {
			t.Errorf("%q", a)
		}
	}
}

func TestNewRecorderSkipsAnEmptyName(t *testing.T) {
	rec := NewRecorder(nil, nil, []string{"", "kiosk-7"})
	if len(rec.patterns) != 1 || rec.matchesPrivateName("") || rec.matchesPrivateName("a - b") ||
		!rec.matchesPrivateName("host kiosk-7 up") {
		t.Fatalf("patterns %v", rec.patterns)
	}
}

func TestKubectlResolverReportsFailures(t *testing.T) {
	tests := []struct {
		script, want string
	}{
		{"#!/bin/sh\necho 'error: the server has no pods' >&2\nexit 1\n",
			"resolve {{pod app=web}}: exit status 1: error: the server has no pods"},
		{"#!/bin/sh\nexit 1\n", "resolve {{pod app=web}}: exit status 1"},
		{"#!/bin/sh\nexit 0\n", "resolve {{pod app=web}}: nothing matched"},
	}
	for _, tt := range tests {
		fakeKubectl(t, tt.script)
		if _, err := KubectlResolver(context.Background(), "pod", "app=web"); err == nil || err.Error() != tt.want {
			t.Errorf("err = %v, want %q", err, tt.want)
		}
	}
}

func TestRecordTaskReportsWhatItCannotStart(t *testing.T) {
	t.Setenv(privateNamesEnvVar, "not-a-real-name")
	if err := RecordTask(context.Background(), Task{ID: "no-record"}, t.TempDir(), nil, io.Discard); err == nil ||
		err.Error() != "no-record: no record block" {
		t.Fatalf("no record block: %v", err)
	}
	bare := Task{ID: "no-setup", Dir: t.TempDir(), Record: &Record{Scenario: "kubernetes/empty"}}
	if err := RecordTask(context.Background(), bare, t.TempDir(), nil, io.Discard); err == nil ||
		err.Error() != "no-setup: scenario kubernetes/empty has no setup.sh" {
		t.Fatalf("no setup.sh: %v", err)
	}
	task, scenarios, _ := newRecordingTask(t, "gone-dir", "- {tool: kubectl, args: {verb: get, resource: pods}}")
	gone := task
	gone.Dir = filepath.Join(t.TempDir(), "gone")
	if err := RecordTask(context.Background(), gone, scenarios, nil, io.Discard); err == nil ||
		!strings.HasPrefix(err.Error(), "crashloop-oomkilled: ") || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a task directory that is gone: %v", err)
	}
	brokenTempDir(t)
	if err := RecordTask(context.Background(), task, scenarios, nil, io.Discard); err == nil ||
		!strings.HasPrefix(err.Error(), "crashloop-oomkilled: ") || !strings.Contains(err.Error(), "taracode-record-") {
		t.Fatalf("no temporary directory: %v", err)
	}
}

// TestRecordTaskStopsOnAPlaceholderItCannotResolve: the call's placeholder failure ends the
// recording, naming the call, and the teardown still runs.
func TestRecordTaskStopsOnAPlaceholderItCannotResolve(t *testing.T) {
	t.Setenv(privateNamesEnvVar, "not-a-real-name")
	task, scenarios, scenario := newRecordingTask(t, "unresolved",
		`- {tool: kubectl, args: {verb: get, resource: pods, namespace: "{{node}}"}}`)
	resolve := func(context.Context, string, string) (string, error) { return "", errors.New("no nodes") }
	err := RecordTask(context.Background(), task, scenarios, resolve, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "crashloop-oomkilled: call 0: ") || !strings.Contains(err.Error(), "no nodes") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(scenario, "teardown-ran")); err != nil {
		t.Fatalf("teardown: %v", err)
	}
}

func TestSwapFixturesReportsWhatItCannotMove(t *testing.T) {
	file := filepath.Join(t.TempDir(), "task")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := swapFixtures(filepath.Join(file, "fixtures"), t.TempDir()); err == nil {
		t.Fatal("a fixtures path under a file")
	}
	// The aside name is the fixtures name plus ".replaced": past the 255-byte name limit, while the
	// fixtures name itself is not.
	final := filepath.Join(t.TempDir(), strings.Repeat("f", 250))
	if err := os.Mkdir(final, 0o755); err != nil {
		t.Fatal(err)
	}
	err := swapFixtures(final, t.TempDir())
	if err == nil || !strings.HasPrefix(err.Error(), "set aside the previous fixtures: ") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(final); statErr != nil {
		t.Fatalf("the previous fixtures stay: %v", statErr)
	}
}

func TestRunScriptWithoutTheScript(t *testing.T) {
	var out bytes.Buffer
	if pgid, err := runScript(context.Background(), t.TempDir(), "teardown.sh", nil, &out); pgid != 0 || err != nil || out.Len() != 0 {
		t.Fatalf("pgid %d, err %v, output %q", pgid, err, out.String())
	}
}

// cancelOnOutput cancels once the script prints "started".
type cancelOnOutput struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.Contains(string(p), "started") {
		w.cancel()
	}
	return w.buf.Write(p)
}

func (w *cancelOnOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestRunScriptStopsAScriptWhenCancelled: cancelling the context while the script runs kills it,
// and runScript reports the failure.
func TestRunScriptStopsAScriptWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setup.sh"), []byte("echo started\nsleep 30\necho finished\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &cancelOnOutput{cancel: cancel}
	if _, err := runScript(ctx, dir, "setup.sh", os.Environ(), out); err == nil || strings.Contains(out.String(), "finished") {
		t.Fatalf("err %v, output %q", err, out.String())
	}
}

// TestKillProcessGroupIgnoresNoGroup: without its guard, pgid 0 would signal the caller's own
// process group. The call runs in a child that leads a process group of its own, so a missing guard
// kills only that child, and the child exiting cleanly is what the test checks.
func TestKillProcessGroupIgnoresNoGroup(t *testing.T) {
	if os.Getenv("TARACODE_TEST_KILL_PGID_ZERO") == "1" {
		killProcessGroup(0)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestKillProcessGroupIgnoresNoGroup$")
	cmd.Env = append(os.Environ(), "TARACODE_TEST_KILL_PGID_ZERO=1",
		"GORACE="+strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the child did not survive killProcessGroup(0): %v\n%s", err, out)
	}
}

func TestCopyDirReportsWhatItCannotCopy(t *testing.T) {
	if err := copyDir(filepath.Join(t.TempDir(), "missing"), t.TempDir()); !os.IsNotExist(err) {
		t.Fatalf("a missing source: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads whatever the mode says")
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "secret.txt"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := copyDir(src, t.TempDir()); !os.IsPermission(err) {
		t.Fatalf("an unreadable file: %v", err)
	}
}

func TestReplayConfinementNeedsTheRunDirectory(t *testing.T) {
	_, reg, runDir := replayRegistry(t)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Execute(context.Background(), "read_file", map[string]any{"path": "notes.txt"}, runDir)
	if err == nil || !strings.Contains(err.Error(), "resolving the run directory") {
		t.Fatalf("err = %v", err)
	}
}

func TestTerraformSigRefusesAnUnterminatedQuote(t *testing.T) {
	if verb, dir, ok := terraformSig(`terraform plan dir="my infra`); ok || verb != "" || dir != "" {
		t.Fatalf("%q %q %v", verb, dir, ok)
	}
}
