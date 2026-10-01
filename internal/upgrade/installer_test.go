package upgrade

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewInstallerAllowsLongDownloads(t *testing.T) {
	c := &Checker{}
	if i := NewInstaller(c); i.checker != c || i.httpClient.Timeout != 5*time.Minute {
		t.Fatalf("%+v", i)
	}
}

func TestUpgradeViaHomebrewAndGo(t *testing.T) {
	tests := []struct {
		method  InstallMethod
		fail    string
		wantErr string
		ran     string
	}{
		{InstallMethodHomebrew, "", "", "brew update|brew upgrade taracode"},
		{InstallMethodHomebrew, "brew update", "brew update failed", "brew update"},
		{InstallMethodHomebrew, "brew upgrade", "brew upgrade failed", "brew update|brew upgrade taracode"},
		{InstallMethodGo, "", "", "go install github.com/tara-vision/taracode@latest"},
		{InstallMethodGo, "go install", "go install failed", "go install github.com/tara-vision/taracode@latest"},
	}
	for _, tt := range tests {
		log := fakeCommands(t, tt.fail, "")
		var err error
		out := captureStdout(t, func() { err = NewInstaller(&Checker{}).Upgrade(&CheckResult{InstallMethod: string(tt.method)}) })
		if (tt.wantErr == "" && err != nil) || (tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr))) {
			t.Errorf("%s with %q failing: err = %v", tt.method, tt.fail, err)
		}
		if got := strings.Join(log.all(), "|"); got != tt.ran {
			t.Errorf("%s with %q failing: ran %s, want %s", tt.method, tt.fail, got, tt.ran)
		}
		if !strings.Contains(out, "Upgrading via") || !strings.Contains(out, "fake "+strings.Split(tt.ran, "|")[0]) {
			t.Errorf("%s: the command's output reaches stdout: %q", tt.method, out)
		}
	}
}

// binaryInstall is a current binary in a temp bin directory, the temp directory moved into the test,
// and a download server for the new binary.
func binaryInstall(t *testing.T, status int, body string) (*Installer, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	current := filepath.Join(t.TempDir(), "bin", "taracode")
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeExecutable(t, current, nil)
	inst := NewInstaller(&Checker{})
	inst.httpClient = clientTo(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "github.com" || r.URL.Path != "/tara-vision/taracode/releases/download/v9.9.9/taracode" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return inst, current
}

const downloadURL = "https://github.com/tara-vision/taracode/releases/download/v9.9.9/taracode"

func TestUpgradeViaBinaryReplacesTheExecutable(t *testing.T) {
	inst, current := binaryInstall(t, http.StatusOK, "new binary")
	log := fakeCommands(t, "", "")
	var err error
	out := captureStdout(t, func() {
		err = inst.Upgrade(&CheckResult{InstallMethod: string(InstallMethodCurl), DownloadURL: downloadURL})
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(current); err != nil || string(data) != "new binary" {
		t.Fatalf("the executable is now %q (%v)", data, err)
	}
	if _, err := os.Stat(current + ".backup"); !os.IsNotExist(err) {
		t.Fatalf("the backup is removed after a good install: %v", err)
	}
	lines := log.all()
	if len(lines) != 1 || !strings.HasSuffix(lines[0], " --version") {
		t.Fatalf("the download is checked with --version before it replaces anything: %v", lines)
	}
	for _, want := range []string{"Downloading from " + downloadURL, "Downloading... 100.0% (10/10 bytes)", "Downloaded: fake "} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
}

func TestUpgradeViaBinaryRefusesABadDownload(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		fail    string
		wantErr string
	}{
		{"missing", http.StatusNotFound, "", "download failed with status 404"},
		{"does not run", http.StatusOK, "--version", "downloaded binary verification failed"},
	}
	for _, tt := range tests {
		inst, current := binaryInstall(t, tt.status, "new binary")
		fakeCommands(t, tt.fail, "")
		var err error
		_ = captureStdout(t, func() { err = inst.upgradeViaBinary(downloadURL) })
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
		if data, _ := os.ReadFile(current); string(data) != "old binary" {
			t.Errorf("%s: a refused download leaves the executable alone: %q", tt.name, data)
		}
	}
}

func TestUpgradeViaBinaryFallsBackToSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root renames whatever the mode says")
	}
	for _, fail := range []string{"", "sudo mv"} {
		inst, current := binaryInstall(t, http.StatusOK, "new binary")
		dir := filepath.Dir(current)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			t.Fatal(err)
		}
		log := fakeCommands(t, fail, "")
		out := captureStdout(t, func() { err = inst.upgradeViaBinary(downloadURL) })
		lines := log.all()
		if len(lines) != 2 || !strings.HasPrefix(lines[1], "sudo mv ") || !strings.HasSuffix(lines[1], " "+resolved) ||
			!strings.Contains(out, "Need elevated permissions to install...") {
			t.Fatalf("sudo failing=%q: ran %v, output %q", fail, lines, out)
		}
		if (fail == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), "sudo mv failed")) {
			t.Fatalf("sudo failing=%q: err = %v", fail, err)
		}
	}
}

func TestUpgradeViaBinaryReportsLocalProblems(t *testing.T) {
	if err := NewInstaller(&Checker{}).Upgrade(&CheckResult{InstallMethod: "anything"}); err == nil ||
		err.Error() != "no download URL available for "+runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("no asset for this platform: %v", err)
	}

	inst, _ := binaryInstall(t, http.StatusOK, "new binary")
	upgradeErr := func() error {
		var err error
		_ = captureStdout(t, func() { err = inst.upgradeViaBinary(downloadURL) })
		return err
	}
	fakeExecutable(t, "", errors.New("no proc"))
	if err := upgradeErr(); err == nil || !strings.Contains(err.Error(), "failed to get executable path") {
		t.Fatalf("err = %v", err)
	}
	fakeExecutable(t, filepath.Join(t.TempDir(), "gone"), nil)
	if err := upgradeErr(); err == nil || !strings.Contains(err.Error(), "failed to resolve symlinks") {
		t.Fatalf("err = %v", err)
	}

	inst, _ = binaryInstall(t, http.StatusOK, "new binary")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	if err := upgradeErr(); err == nil || !strings.Contains(err.Error(), "failed to create temp file") {
		t.Fatalf("err = %v", err)
	}

	inst, _ = binaryInstall(t, http.StatusOK, "new binary")
	inst.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network is down")
	})}
	if err := upgradeErr(); err == nil || !strings.Contains(err.Error(), "failed to download") {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultConfigChecksDailyAndAsksFirst(t *testing.T) {
	if got := DefaultConfig(); got != (UpgradeConfig{AutoCheck: true, CheckInterval: 24 * time.Hour, ShowChangelog: true}) {
		t.Fatalf("%+v", got)
	}
}

func TestUpgradeViaBinaryReportsATruncatedDownload(t *testing.T) {
	inst, current := binaryInstall(t, http.StatusOK, "")
	inst.httpClient = clientTo(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
	})
	var err error
	_ = captureStdout(t, func() { err = inst.upgradeViaBinary(downloadURL) })
	if err == nil || !strings.Contains(err.Error(), "failed to save download") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(current); string(data) != "old binary" {
		t.Fatalf("the executable is untouched: %q", data)
	}
}

func TestWriteCounterReportsProgress(t *testing.T) {
	known := &writeCounter{Total: 4}
	out := captureStdout(t, func() {
		if n, err := known.Write([]byte("ab")); n != 2 || err != nil {
			t.Errorf("Write() = %d, %v", n, err)
		}
	})
	if out != "\rDownloading... 50.0% (2/4 bytes)" || known.Downloaded != 2 {
		t.Fatalf("%q, %d", out, known.Downloaded)
	}
	unknown := &writeCounter{Total: -1}
	if out := captureStdout(t, func() { _, _ = unknown.Write([]byte("abc")) }); out != "\rDownloading... 3 bytes" {
		t.Fatalf("%q", out)
	}
}
