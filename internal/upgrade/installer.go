package upgrade

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Installer handles the upgrade process
type Installer struct {
	checker    *Checker
	httpClient *http.Client
}

// NewInstaller creates a new installer
func NewInstaller(checker *Checker) *Installer {
	return &Installer{
		checker: checker,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Longer timeout for downloads
		},
	}
}

// Upgrade performs the upgrade based on install method
func (i *Installer) Upgrade(result *CheckResult) error {
	method := InstallMethod(result.InstallMethod)

	switch method {
	case InstallMethodHomebrew:
		return i.upgradeViaHomebrew()
	case InstallMethodGo:
		return i.upgradeViaGo()
	case InstallMethodCurl, InstallMethodManual, InstallMethodUnknown:
		return i.upgradeViaBinary(result.DownloadURL)
	default:
		return i.upgradeViaBinary(result.DownloadURL)
	}
}

// upgradeViaHomebrew upgrades using Homebrew
func (i *Installer) upgradeViaHomebrew() error {
	fmt.Println("Upgrading via Homebrew...")

	// First update the tap
	cmd := exec.Command("brew", "update")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew update failed: %w", err)
	}

	// Then upgrade taracode
	cmd = exec.Command("brew", "upgrade", "taracode")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew upgrade failed: %w", err)
	}

	return nil
}

// upgradeViaGo upgrades using go install
func (i *Installer) upgradeViaGo() error {
	fmt.Println("Upgrading via go install...")

	cmd := exec.Command("go", "install", "github.com/tara-vision/taracode@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}

	return nil
}

// upgradeViaBinary downloads and installs the binary directly
func (i *Installer) upgradeViaBinary(downloadURL string) error {
	if downloadURL == "" {
		return fmt.Errorf("no download URL available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("Downloading from %s...\n", downloadURL)

	// Get the current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "taracode-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Download the binary
	resp, err := i.httpClient.Get(downloadURL)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Show download progress
	counter := &writeCounter{Total: resp.ContentLength}
	_, err = io.Copy(tmpFile, io.TeeReader(resp.Body, counter))
	tmpFile.Close()
	if err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}
	fmt.Println() // New line after progress

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to make executable: %w", err)
	}

	// Verify the downloaded binary works
	cmd := exec.Command(tmpPath, "--version")
	if output, err := cmd.Output(); err != nil {
		return fmt.Errorf("downloaded binary verification failed: %w", err)
	} else {
		fmt.Printf("Downloaded: %s", output)
	}

	// Replace the current binary
	// First, try to backup the old binary
	backupPath := execPath + ".backup"
	os.Remove(backupPath) // Remove old backup if exists
	if err := os.Rename(execPath, backupPath); err != nil {
		// If rename fails, try with sudo
		return i.replaceWithSudo(tmpPath, execPath)
	}

	// Move new binary to target location
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Restore backup
		os.Rename(backupPath, execPath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Remove backup on success
	os.Remove(backupPath)

	return nil
}

// replaceWithSudo attempts to replace the binary using sudo
func (i *Installer) replaceWithSudo(srcPath, dstPath string) error {
	fmt.Println("Need elevated permissions to install...")

	cmd := exec.Command("sudo", "mv", srcPath, dstPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo mv failed: %w", err)
	}

	return nil
}

// writeCounter counts bytes written and shows progress
type writeCounter struct {
	Total      int64
	Downloaded int64
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Downloaded += int64(n)

	// Print progress
	if wc.Total > 0 {
		percent := float64(wc.Downloaded) / float64(wc.Total) * 100
		fmt.Printf("\rDownloading... %.1f%% (%d/%d bytes)", percent, wc.Downloaded, wc.Total)
	} else {
		fmt.Printf("\rDownloading... %d bytes", wc.Downloaded)
	}

	return n, nil
}

// GetUpgradeCommand returns the appropriate upgrade command for the user
func GetUpgradeCommand(method InstallMethod) string {
	switch method {
	case InstallMethodHomebrew:
		return "brew upgrade taracode"
	case InstallMethodGo:
		return "go install github.com/tara-vision/taracode@latest"
	case InstallMethodCurl:
		return "curl -fsSL https://code.tara.vision/install.sh | bash"
	default:
		return "curl -fsSL https://code.tara.vision/install.sh | bash"
	}
}
