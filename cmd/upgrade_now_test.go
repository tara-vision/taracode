package cmd

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/manifoldco/promptui"

	"github.com/tara-vision/taracode/internal/upgrade"
)

// upgradeAnswers stands in for the confirmation prompt (answering err, nil = yes) and for the
// installer (failing with installErr); it returns what the installer was asked to install, if it
// was asked at all, and the prompt it was shown.
func upgradeAnswers(t *testing.T, err, installErr error) (**upgrade.CheckResult, *promptui.Prompt) {
	t.Helper()
	var installed *upgrade.CheckResult
	shown := &promptui.Prompt{}
	prompt, install := runPrompt, installUpgrade
	runPrompt = func(p *promptui.Prompt) (string, error) {
		*shown = *p
		return "y", err
	}
	installUpgrade = func(_ *upgrade.Checker, result *upgrade.CheckResult) error {
		installed = result
		return installErr
	}
	t.Cleanup(func() { runPrompt, installUpgrade = prompt, install })
	return &installed, shown
}

func TestUpgradeNowOnTheLatestVersion(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	withVersion(t, "9.9.9")
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	installed, _ := upgradeAnswers(t, nil, nil)
	if out := runUpgrade(t, "now"); !strings.Contains(out, "You're already running the latest version (9.9.9)") || *installed != nil {
		t.Fatalf("%q", out)
	}
}

func TestUpgradeNowCanBeCancelled(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	withVersion(t, "1.0.0")
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	installed, shown := upgradeAnswers(t, promptui.ErrAbort, nil)
	out := runUpgrade(t, "now")
	if !strings.Contains(out, "Upgrading from 1.0.0 to v9.9.9") || !strings.Contains(out, "Upgrade cancelled") || *installed != nil {
		t.Fatalf("%q", out)
	}
	if shown.Label != "Proceed with upgrade" || !shown.IsConfirm || shown.Default != "y" {
		t.Fatalf("the prompt %+v", shown)
	}
}

func TestUpgradeNowInstallsTheRelease(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{})
	withVersion(t, "1.0.0")
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	installed, _ := upgradeAnswers(t, nil, nil)
	out := runUpgrade(t, "now")
	if !strings.Contains(out, "Successfully upgraded to v9.9.9!") || !strings.Contains(out, "Please restart taracode to use the new version.") {
		t.Fatalf("%q", out)
	}
	if *installed == nil || (*installed).LatestVersion != "v9.9.9" || (*installed).InstallMethod != "curl" {
		t.Fatalf("installed %+v", *installed)
	}
}

func TestUpgradeNowReportsAFailedInstall(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{InstallMethod: string(upgrade.InstallMethodHomebrew)})
	withVersion(t, "1.0.0")
	githubAnswers(t, http.StatusOK, release("v9.9.9", "notes"))
	upgradeAnswers(t, nil, errors.New("brew upgrade failed"))
	out := runUpgrade(t, "now")
	for _, want := range []string{"Upgrade failed: brew upgrade failed", "You can try upgrading manually:", "  brew upgrade taracode"} {
		if !strings.Contains(out, want) {
			t.Errorf("/upgrade now lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Successfully") {
		t.Errorf("a failed install is not a success:\n%s", out)
	}
}

func TestShowUpdateBanner(t *testing.T) {
	for _, quiet := range []*upgrade.CheckResult{
		nil, {UpdateAvailable: false}, {UpdateAvailable: true, SkippedByUser: true},
	} {
		if out := captureStdoutForTest(t, func() { ShowUpdateBanner(quiet) }); out != "" {
			t.Errorf("ShowUpdateBanner(%+v) printed %q", quiet, out)
		}
	}
	out := captureStdoutForTest(t, func() {
		ShowUpdateBanner(&upgrade.CheckResult{CurrentVersion: "3.2.0", LatestVersion: "v3.3.0", UpdateAvailable: true})
	})
	for _, want := range []string{"Update available: 3.2.0 -> v3.3.0", "Run '/upgrade' for details or '/upgrade now' to install"} {
		if !strings.Contains(out, want) {
			t.Errorf("banner lacks %q:\n%s", want, out)
		}
	}
}

// awaitUpdateCheck runs CheckForUpdateAsync and waits for its answer.
func awaitUpdateCheck(t *testing.T, version string) *upgrade.CheckResult {
	t.Helper()
	results := make(chan *upgrade.CheckResult, 1)
	CheckForUpdateAsync(version, results)
	select {
	case result := <-results:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("the update check never answered")
		return nil
	}
}

// TestCheckForUpdateAsyncUsesARecentCheck: within a day of the last check the stored answer is used
// and nothing is fetched (the fake internet would fail every request).
func TestCheckForUpdateAsyncUsesARecentCheck(t *testing.T) {
	githubAnswers(t, http.StatusServiceUnavailable, nil)
	tests := []struct {
		name  string
		state upgrade.UpgradeState
		want  *upgrade.CheckResult
	}{
		{"newer", upgrade.UpgradeState{LastCheckTime: time.Now(), LastCheckVersion: "v3.3.0"},
			&upgrade.CheckResult{CurrentVersion: "v3.2.0", LatestVersion: "v3.3.0", UpdateAvailable: true}},
		{"newer but skipped", upgrade.UpgradeState{LastCheckTime: time.Now(), LastCheckVersion: "v3.3.0", SkippedVersion: "v3.3.0"},
			&upgrade.CheckResult{CurrentVersion: "v3.2.0", LatestVersion: "v3.3.0", UpdateAvailable: true, SkippedByUser: true}},
		{"same", upgrade.UpgradeState{LastCheckTime: time.Now(), LastCheckVersion: "v3.2.0"}, nil},
		{"nothing stored", upgrade.UpgradeState{LastCheckTime: time.Now()}, nil},
	}
	for _, tt := range tests {
		upgradeHome(t, tt.state)
		got := awaitUpdateCheck(t, "v3.2.0")
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%s: CheckForUpdateAsync() = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestCheckForUpdateAsyncFetchesAStaleCheck(t *testing.T) {
	upgradeHome(t, upgrade.UpgradeState{LastCheckTime: time.Now().Add(-48 * time.Hour), LastCheckVersion: "v3.2.0"})
	githubAnswers(t, http.StatusOK, release("v3.4.0", "notes"))
	if got := awaitUpdateCheck(t, "v3.2.0"); got == nil || got.LatestVersion != "v3.4.0" || !got.UpdateAvailable {
		t.Fatalf("CheckForUpdateAsync() = %+v", got)
	}

	upgradeHome(t, upgrade.UpgradeState{})
	githubAnswers(t, http.StatusServiceUnavailable, nil)
	if got := awaitUpdateCheck(t, "v3.2.0"); got != nil {
		t.Fatalf("a failed fetch answers nil: %+v", got)
	}
}

func TestTruncateChangelog(t *testing.T) {
	if got := truncateChangelog("short notes", 500); got != "short notes" {
		t.Fatalf("%q", got)
	}
	lines := strings.Repeat("0123456789\n", 10) // 110 characters, a newline every 11
	if got := truncateChangelog(lines, 50); got != strings.Repeat("0123456789\n", 3)+"0123456789\n..." {
		t.Fatalf("cut at the last newline before 50: %q", got)
	}
	flat := strings.Repeat("x", 120)
	if got := truncateChangelog(flat, 50); got != strings.Repeat("x", 50)+"\n..." {
		t.Fatalf("no newline in the second half: cut at 50: %q", got)
	}
	early := "short\n" + strings.Repeat("x", 60)
	if got := truncateChangelog(early, 50); got != "short\n"+strings.Repeat("x", 44)+"\n..." {
		t.Fatalf("a newline in the first half is no cut point: cut at 50: %q", got)
	}
}

// The default installer is the upgrade package's: a release with no download for this platform is
// refused before anything is fetched, so this test never replaces the binary that runs it.
func TestTheDefaultInstallerRefusesAReleaseWithoutADownload(t *testing.T) {
	err := installUpgrade(nil, &upgrade.CheckResult{InstallMethod: string(upgrade.InstallMethodCurl)})
	if err == nil || !strings.Contains(err.Error(), "no download URL available") {
		t.Fatalf("installUpgrade = %v, want the refusal for a missing download URL", err)
	}
}
