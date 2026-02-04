package upgrade

import "time"

// ReleaseInfo contains information about a GitHub release
type ReleaseInfo struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset represents a release asset (binary file)
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpgradeState stores persistent upgrade state
type UpgradeState struct {
	LastCheckTime    time.Time `json:"last_check_time"`
	LastCheckVersion string    `json:"last_check_version"`
	SkippedVersion   string    `json:"skipped_version,omitempty"`
	InstallMethod    string    `json:"install_method,omitempty"` // curl, homebrew, go, manual
}

// CheckResult contains the result of a version check
type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	ReleaseInfo     *ReleaseInfo
	Changelog       string
	DownloadURL     string
	InstallMethod   string
	SkippedByUser   bool
}

// InstallMethod represents how taracode was installed
type InstallMethod string

const (
	InstallMethodCurl     InstallMethod = "curl"
	InstallMethodHomebrew InstallMethod = "homebrew"
	InstallMethodGo       InstallMethod = "go"
	InstallMethodManual   InstallMethod = "manual"
	InstallMethodUnknown  InstallMethod = "unknown"
)

// UpgradeConfig holds upgrade-related configuration
type UpgradeConfig struct {
	AutoCheck     bool          `mapstructure:"auto_check"`
	CheckInterval time.Duration `mapstructure:"check_interval"`
	AutoUpgrade   bool          `mapstructure:"auto_upgrade"`
	ShowChangelog bool          `mapstructure:"show_changelog"`
}

// DefaultConfig returns the default upgrade configuration
func DefaultConfig() UpgradeConfig {
	return UpgradeConfig{
		AutoCheck:     true,
		CheckInterval: 24 * time.Hour,
		AutoUpgrade:   false, // Require user confirmation by default
		ShowChangelog: true,
	}
}
