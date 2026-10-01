package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testChecker is a checker on version with its state in a fresh directory and its requests sent to
// handler. Install detection finds no brew and an executable in a temporary directory, so it never
// looks at the machine the tests run on; a test that needs other answers fakes them after this.
func testChecker(t *testing.T, version string, handler http.HandlerFunc) *Checker {
	t.Helper()
	fakeLookPath(t, false)
	fakeExecutable(t, filepath.Join(t.TempDir(), "taracode"), nil)
	return &Checker{currentVersion: version, stateDir: filepath.Join(t.TempDir(), ".taracode"), httpClient: clientTo(t, handler)}
}

// releaseHandler answers GitHub's latest-release URL with release and records the request headers.
func releaseHandler(t *testing.T, release map[string]any, seen *http.Header) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.github.com" || r.URL.Path != "/repos/tara-vision/taracode/releases/latest" {
			http.NotFound(w, r)
			return
		}
		if seen != nil {
			*seen = r.Header.Clone()
		}
		_ = json.NewEncoder(w).Encode(release)
	}
}

func platformAsset() string { return fmt.Sprintf("taracode-%s-%s", runtime.GOOS, runtime.GOARCH) }

func TestNewCheckerKeepsItsStateInTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := NewChecker("1.0.0")
	if c.currentVersion != "1.0.0" || c.stateDir != filepath.Join(home, ".taracode") || c.httpClient.Timeout != RequestTimeout {
		t.Fatalf("%+v", c)
	}
}

func TestCheckForUpdateReadsTheLatestRelease(t *testing.T) {
	var headers http.Header
	release := map[string]any{"tag_name": "v2.0.0", "body": "notes", "html_url": "https://github.com/x",
		"assets": []map[string]any{
			{"name": "taracode-plan9-mips", "browser_download_url": "https://example.com/other"},
			{"name": platformAsset(), "browser_download_url": "https://example.com/mine"},
		}}
	c := testChecker(t, "1.0.0", releaseHandler(t, release, &headers))
	if err := c.SetInstallMethod(InstallMethodCurl); err != nil {
		t.Fatal(err)
	}
	res, err := c.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if res.CurrentVersion != "1.0.0" || res.LatestVersion != "v2.0.0" || !res.UpdateAvailable || res.Changelog != "notes" ||
		res.DownloadURL != "https://example.com/mine" || res.InstallMethod != "curl" || res.SkippedByUser ||
		res.ReleaseInfo == nil || res.ReleaseInfo.HTMLURL != "https://github.com/x" {
		t.Fatalf("result %+v", res)
	}
	if headers.Get("Accept") != "application/vnd.github.v3+json" || headers.Get("User-Agent") != "taracode/1.0.0" {
		t.Fatalf("request headers %v", headers)
	}
	state := c.LoadState()
	if state.LastCheckVersion != "v2.0.0" || time.Since(state.LastCheckTime) > time.Minute || state.InstallMethod != "curl" {
		t.Fatalf("the check is recorded and the install method kept: %+v", state)
	}

	if err := c.SkipVersion("v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if res, err := c.CheckForUpdate(); err != nil || !res.SkippedByUser {
		t.Fatalf("a skipped version: %+v %v", res, err)
	}
}

func TestCheckForUpdateOnTheLatestVersionWithoutAnAsset(t *testing.T) {
	c := testChecker(t, "v2.0.0", releaseHandler(t, map[string]any{"tag_name": "v2.0.0"}, nil))
	if err := c.SetInstallMethod(InstallMethodGo); err != nil {
		t.Fatal(err)
	}
	res, err := c.CheckForUpdate()
	if err != nil || res.UpdateAvailable || res.DownloadURL != "" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestCheckForUpdateErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
			"GitHub API returned status 503"},
		{"not json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, "invalid character"},
	}
	for _, tt := range tests {
		c := testChecker(t, "1.0.0", tt.handler)
		if _, err := c.CheckForUpdate(); err == nil || !strings.Contains(err.Error(), "failed to check for updates: ") ||
			!strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
	c := testChecker(t, "1.0.0", nil)
	c.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network is down")
	})}
	if _, err := c.CheckForUpdate(); err == nil || !strings.Contains(err.Error(), "network is down") {
		t.Fatalf("err = %v", err)
	}
}

func TestDetectInstallMethod(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	tests := []struct {
		name       string
		saved      InstallMethod
		brew       bool
		fail       string
		silent     string
		executable string
		exeErr     error
		gobin      string
		gopath     string
		want       InstallMethod
	}{
		{name: "saved", saved: InstallMethodManual, want: InstallMethodManual},
		{name: "homebrew", brew: true, executable: "/opt/homebrew/bin/taracode", want: InstallMethodHomebrew},
		{name: "brew without taracode", brew: true, fail: "brew list", executable: "/opt/x/taracode", want: InstallMethodUnknown},
		{name: "brew lists nothing", brew: true, silent: "brew list", executable: "/opt/x/taracode", want: InstallMethodUnknown},
		{name: "gobin", executable: "/work/gobin/taracode", gobin: "/work/gobin", want: InstallMethodGo},
		{name: "gopath", executable: "/work/gopath/bin/taracode", gopath: "/work/gopath", want: InstallMethodGo},
		{name: "default gopath", executable: filepath.Join(home, "go", "bin", "taracode"), want: InstallMethodGo},
		{name: "curl", executable: "/usr/local/bin/taracode", want: InstallMethodCurl},
		{name: "unknown", executable: "/opt/elsewhere/taracode", want: InstallMethodUnknown},
		{name: "no executable", exeErr: errors.New("no proc"), want: InstallMethodUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testChecker(t, "1.0.0", nil)
			fakeCommands(t, tt.fail, tt.silent)
			fakeLookPath(t, tt.brew)
			fakeExecutable(t, tt.executable, tt.exeErr)
			t.Setenv("GOBIN", tt.gobin)
			t.Setenv("GOPATH", tt.gopath)
			if tt.saved != "" {
				if err := c.SetInstallMethod(tt.saved); err != nil {
					t.Fatal(err)
				}
			}
			if got := c.DetectInstallMethod(); got != tt.want {
				t.Fatalf("DetectInstallMethod() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestShouldCheckAndTheState(t *testing.T) {
	c := testChecker(t, "1.0.0", nil)
	if !c.ShouldCheck(time.Hour) || c.LoadState().LastCheckVersion != "" {
		t.Fatal("never checked: check now")
	}
	if err := c.SaveState(&UpgradeState{LastCheckTime: time.Now(), LastCheckVersion: "v1.1.0"}); err != nil {
		t.Fatal(err)
	}
	if c.ShouldCheck(time.Hour) || !c.ShouldCheck(0) {
		t.Fatal("a recent check waits for the interval")
	}
	if err := c.SkipVersion("v1.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := c.ClearSkippedVersion(); err != nil || c.LoadState().SkippedVersion != "" || c.LoadState().LastCheckVersion != "v1.1.0" {
		t.Fatalf("clearing the skip keeps the rest: %+v %v", c.LoadState(), err)
	}
	if err := os.WriteFile(filepath.Join(c.stateDir, StateFileName), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state := c.LoadState(); state.LastCheckVersion != "" || !state.LastCheckTime.IsZero() {
		t.Fatalf("a broken state file reads as empty: %+v", state)
	}
}

func TestSaveStateErrors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{stateDir: filepath.Join(blocker, "state")}
	if err := c.SaveState(&UpgradeState{}); err == nil {
		t.Fatal("a state directory under a file")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	c = &Checker{stateDir: readOnly}
	if err := c.SkipVersion("v1"); err == nil {
		t.Fatal("a read-only state directory")
	}
}
