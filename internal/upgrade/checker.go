package upgrade

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	GitHubRepo     = "tara-vision/taracode"
	GitHubAPIURL   = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	StateFileName  = "upgrade_state.json"
	RequestTimeout = 10 * time.Second
)

// Checker handles version checking
type Checker struct {
	currentVersion string
	stateDir       string
	httpClient     *http.Client
}

// NewChecker creates a new version checker
func NewChecker(currentVersion string) *Checker {
	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".taracode")

	return &Checker{
		currentVersion: currentVersion,
		stateDir:       stateDir,
		httpClient: &http.Client{
			Timeout: RequestTimeout,
		},
	}
}

// CheckForUpdate checks if a new version is available
func (c *Checker) CheckForUpdate() (*CheckResult, error) {
	// Fetch latest release info from GitHub
	releaseInfo, err := c.fetchLatestRelease()
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(releaseInfo.TagName, "v")
	currentVersion := strings.TrimPrefix(c.currentVersion, "v")

	// Check if user skipped this version
	state := c.LoadState()
	skippedByUser := state.SkippedVersion == releaseInfo.TagName

	result := &CheckResult{
		CurrentVersion:  c.currentVersion,
		LatestVersion:   releaseInfo.TagName,
		UpdateAvailable: isNewerVersion(latestVersion, currentVersion),
		ReleaseInfo:     releaseInfo,
		Changelog:       releaseInfo.Body,
		DownloadURL:     c.getDownloadURL(releaseInfo),
		InstallMethod:   string(c.DetectInstallMethod()),
		SkippedByUser:   skippedByUser,
	}

	// Update last check time
	state.LastCheckTime = time.Now()
	state.LastCheckVersion = releaseInfo.TagName
	c.SaveState(state)

	return result, nil
}

// fetchLatestRelease fetches the latest release info from GitHub API
func (c *Checker) fetchLatestRelease() (*ReleaseInfo, error) {
	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "taracode/"+c.currentVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// getDownloadURL returns the appropriate download URL for the current platform
func (c *Checker) getDownloadURL(release *ReleaseInfo) string {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Map Go arch names to binary names
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	}

	binaryName := fmt.Sprintf("taracode-%s-%s", osName, arch)

	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			return asset.BrowserDownloadURL
		}
	}

	return ""
}

// DetectInstallMethod attempts to detect how taracode was installed
func (c *Checker) DetectInstallMethod() InstallMethod {
	// Check saved state first
	state := c.LoadState()
	if state.InstallMethod != "" {
		return InstallMethod(state.InstallMethod)
	}

	// Check if installed via Homebrew
	if c.isHomebrewInstall() {
		return InstallMethodHomebrew
	}

	// Check if installed via go install (in GOPATH/bin or GOBIN)
	if c.isGoInstall() {
		return InstallMethodGo
	}

	// Check if binary is in /usr/local/bin (typical curl install location)
	execPath, err := os.Executable()
	if err == nil {
		if strings.HasPrefix(execPath, "/usr/local/bin") {
			return InstallMethodCurl
		}
	}

	return InstallMethodUnknown
}

// isHomebrewInstall checks if taracode was installed via Homebrew
func (c *Checker) isHomebrewInstall() bool {
	// Check if brew command exists
	_, err := exec.LookPath("brew")
	if err != nil {
		return false
	}

	// Check if taracode is in Homebrew's Cellar
	cmd := exec.Command("brew", "list", "--versions", "taracode")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return len(output) > 0
}

// isGoInstall checks if taracode was installed via go install
func (c *Checker) isGoInstall() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}

	// Check GOBIN
	gobin := os.Getenv("GOBIN")
	if gobin != "" && strings.HasPrefix(execPath, gobin) {
		return true
	}

	// Check GOPATH/bin
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		homeDir, _ := os.UserHomeDir()
		gopath = filepath.Join(homeDir, "go")
	}
	goBin := filepath.Join(gopath, "bin")
	if strings.HasPrefix(execPath, goBin) {
		return true
	}

	return false
}

// ShouldCheck returns true if enough time has passed since the last check
func (c *Checker) ShouldCheck(interval time.Duration) bool {
	state := c.LoadState()
	if state.LastCheckTime.IsZero() {
		return true
	}
	return time.Since(state.LastCheckTime) >= interval
}

// LoadState loads the upgrade state from disk
func (c *Checker) LoadState() *UpgradeState {
	statePath := filepath.Join(c.stateDir, StateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		return &UpgradeState{}
	}

	var state UpgradeState
	if err := json.Unmarshal(data, &state); err != nil {
		return &UpgradeState{}
	}

	return &state
}

// SaveState saves the upgrade state to disk
func (c *Checker) SaveState(state *UpgradeState) error {
	if err := os.MkdirAll(c.stateDir, 0755); err != nil {
		return err
	}

	statePath := filepath.Join(c.stateDir, StateFileName)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statePath, data, 0644)
}

// SkipVersion marks a version as skipped by the user
func (c *Checker) SkipVersion(version string) error {
	state := c.LoadState()
	state.SkippedVersion = version
	return c.SaveState(state)
}

// ClearSkippedVersion clears the skipped version
func (c *Checker) ClearSkippedVersion() error {
	state := c.LoadState()
	state.SkippedVersion = ""
	return c.SaveState(state)
}

// SetInstallMethod saves the detected/specified install method
func (c *Checker) SetInstallMethod(method InstallMethod) error {
	state := c.LoadState()
	state.InstallMethod = string(method)
	return c.SaveState(state)
}

// isNewerVersion compares two semantic versions (without 'v' prefix)
// Returns true if latest > current
func isNewerVersion(latest, current string) bool {
	// Handle dev version - always consider updates available
	if current == "dev" || current == "" {
		return true
	}

	latestParts := parseVersion(latest)
	currentParts := parseVersion(current)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return false
}

// parseVersion parses a semantic version string into [major, minor, patch]
func parseVersion(version string) [3]int {
	parts := strings.Split(version, ".")
	var result [3]int

	for i := 0; i < 3 && i < len(parts); i++ {
		// Remove any suffix (like -beta, -rc1)
		numStr := strings.Split(parts[i], "-")[0]
		num, _ := strconv.Atoi(numStr)
		result[i] = num
	}

	return result
}

// CompareVersions compares two versions and returns:
// -1 if v1 < v2
//
//	0 if v1 == v2
//	1 if v1 > v2
func CompareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := parseVersion(v1)
	parts2 := parseVersion(v2)

	for i := 0; i < 3; i++ {
		if parts1[i] < parts2[i] {
			return -1
		}
		if parts1[i] > parts2[i] {
			return 1
		}
	}

	return 0
}
