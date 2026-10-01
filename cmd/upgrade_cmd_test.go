package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manifoldco/promptui"

	"github.com/tara-vision/taracode/internal/upgrade"
)

const latestReleasePath = "/repos/tara-vision/taracode/releases/latest"

// githubAnswers serves the latest-release check through fakeInternet: release as JSON, or status
// when it is not 200. Anything but GitHub's latest-release URL gets a 404.
func githubAnswers(t *testing.T, status int, release map[string]any) {
	t.Helper()
	fakeInternet(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.github.com" || r.URL.Path != latestReleasePath {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			http.Error(w, "unavailable", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	})
}

// release is a latest-release answer for tag with body as its notes.
func release(tag, body string) map[string]any {
	return map[string]any{"tag_name": tag, "body": body, "published_at": "2026-09-30T08:00:00Z",
		"html_url": "https://github.com/tara-vision/taracode/releases/tag/" + tag}
}

// upgradeHome isolates HOME and writes state as its upgrade state. An install method is always set,
// so DetectInstallMethod never looks for brew on the machine running the tests.
func upgradeHome(t *testing.T, state upgrade.UpgradeState) string {
	t.Helper()
	home := isolateHome(t)
	if state.InstallMethod == "" {
		state.InstallMethod = string(upgrade.InstallMethodCurl)
	}
	dir := filepath.Join(home, ".taracode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, upgrade.StateFileName)
	writeFile(t, path, string(data))
	return path
}

func readState(t *testing.T, path string) upgrade.UpgradeState {
	t.Helper()
	var state upgrade.UpgradeState
	if err := json.Unmarshal([]byte(readFile(t, path)), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

// runUpgrade dispatches "/upgrade args" and returns what it printed.
func runUpgrade(t *testing.T, args string) string {
	t.Helper()
	return captureStdoutForTest(t, func() { (&repl{}).dispatch(strings.TrimSpace("/upgrade " + args)) })
}

func TestUpgradeCheckReportsAFailedCheck(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	githubAnswers(t, http.StatusServiceUnavailable, nil)
	installed, _ := upgradeAnswers(t, promptui.ErrAbort, errors.New("must not install"))
	for _, args := range []string{"", "check", "now", "skip"} {
		if out := runUpgrade(t, args); !strings.Contains(out, "Failed to check for updates") ||
			!strings.Contains(out, "GitHub API returned status 503") {
			t.Errorf("/upgrade %s: %q", args, out)
		}
	}
	if *installed != nil {
		t.Errorf("a failed check installed %+v", *installed)
	}
	if out := runUpgrade(t, "changelog"); !strings.Contains(out, "Failed to fetch changelog") {
		t.Errorf("/upgrade changelog: %q", out)
	}
}

func TestUpgradeCheckOnTheLatestVersion(t *testing.T) {
	path := upgradeHome(t, upgrade.UpgradeState{})
	withVersion(t, "9.9.9")
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	out := runUpgrade(t, "check")
	for _, want := range []string{
		"Checking for Updates", "Current version:  9.9.9", "Latest version:   v9.9.9", "Install method:   curl",
		"You're running the latest version!",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade check lacks %q:\n%s", want, out)
		}
	}
	if state := readState(t, path); state.LastCheckVersion != "v9.9.9" || state.LastCheckTime.IsZero() {
		t.Fatalf("the check is recorded: %+v", state)
	}
}

func TestUpgradeCheckRemembersASkippedVersion(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{SkippedVersion: "v9.9.9"})
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	out := runUpgrade(t, "")
	if !strings.Contains(out, "Update available (you previously skipped this version)") ||
		!strings.Contains(out, "Use '/upgrade now' to upgrade anyway") || strings.Contains(out, "Changelog:") {
		t.Fatalf("%q", out)
	}
}

func TestUpgradeCheckShowsTheChangelogAndTheCommand(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	notes := "## What's new\n" + strings.Repeat("- a fix in the shell tool\n", 30) + "THE TAIL"
	githubAnswers(t, http.StatusOK, release("v9.9.9", notes))
	out := runUpgrade(t, "check")
	for _, want := range []string{
		"A new version is available!", "Changelog:", "What's new", "...",
		"To upgrade, run:", upgrade.GetUpgradeCommand(upgrade.InstallMethodCurl), "Or use '/upgrade now' to upgrade automatically",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade check lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "THE TAIL") {
		t.Errorf("the changelog preview is cut at 500 characters:\n%s", out)
	}
}

func TestUpgradeSkipRecordsTheVersion(t *testing.T) {
	path := upgradeHome(t, upgrade.UpgradeState{})
	withVersion(t, "9.9.9")
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	if out := runUpgrade(t, "skip"); !strings.Contains(out, "No update available to skip") {
		t.Fatalf("%q", out)
	}
	withVersion(t, "1.0.0")
	out := runUpgrade(t, "skip")
	if !strings.Contains(out, "Skipped version v9.9.9") || !strings.Contains(out, "You won't be notified about this version again") {
		t.Fatalf("%q", out)
	}
	if state := readState(t, path); state.SkippedVersion != "v9.9.9" {
		t.Fatalf("state %+v", state)
	}
}

func TestUpgradeSkipReportsAStateWriteError(t *testing.T) {
	skipIfRoot(t)
	path := upgradeHome(t, upgrade.UpgradeState{})
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	if out := runUpgrade(t, "skip"); !strings.Contains(out, "Failed to skip version:") {
		t.Fatalf("%q", out)
	}
}

func TestUpgradeChangelogShowsTheReleaseNotes(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	githubAnswers(t, http.StatusOK, release("v9.9.9", "- faster shell tool"))
	out := runUpgrade(t, "changelog")
	for _, want := range []string{
		"Release Changelog", "Version: v9.9.9", "Released: 2026-09-30", "- faster shell tool",
		"View on GitHub: https://github.com/tara-vision/taracode/releases/tag/v9.9.9",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade changelog lacks %q:\n%s", want, out)
		}
	}
}

func TestUpgradeChangelogOfASparseRelease(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	githubAnswers(t, http.StatusOK, map[string]any{"tag_name": "v9.9.9"})
	out := runUpgrade(t, "changelog")
	if !strings.Contains(out, "No changelog available") || strings.Contains(out, "Released:") ||
		strings.Contains(out, "View on GitHub") {
		t.Fatalf("%q", out)
	}
}

func TestUpgradeStatusShowsTheState(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{InstallMethod: string(upgrade.InstallMethodGo)})
	withVersion(t, "3.2.0")
	out := runUpgrade(t, "status")
	for _, want := range []string{"Upgrade Status", "Current version:    3.2.0", "Install method:     go", "Last check:         never"} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Latest known") || strings.Contains(out, "Skipped version") {
		t.Errorf("nothing checked or skipped yet:\n%s", out)
	}

	checked := time.Date(2026, 9, 30, 8, 15, 0, 0, time.Local)
	upgradeHome(t, upgrade.UpgradeState{LastCheckTime: checked, LastCheckVersion: "v3.3.0", SkippedVersion: "v3.3.0"})
	out = runUpgrade(t, "status")
	for _, want := range []string{"Last check:         2026-09-30 08:15:00", "Latest known:       v3.3.0", "Skipped version:    v3.3.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade status lacks %q:\n%s", want, out)
		}
	}
}

func TestUpgradeHelpAndUnknownSubcommands(t *testing.T) {
	out := runUpgrade(t, "HELP")
	for _, want := range []string{"Upgrade Commands", "/upgrade now       Download and install the latest version",
		"/upgrade help      Show this help message"} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade help lacks %q:\n%s", want, out)
		}
	}
	if out := runUpgrade(t, "sideways"); !strings.Contains(out, "Unknown upgrade subcommand: sideways") ||
		!strings.Contains(out, "Use '/upgrade help' for available commands") {
		t.Fatalf("%q", out)
	}
}
