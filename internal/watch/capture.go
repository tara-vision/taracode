package watch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// displayInfo holds information about a display
type displayInfo struct {
	ID     int
	Width  int
	Height int
	Name   string
}

// getDisplays returns information about all connected displays using system_profiler
func getDisplays() ([]displayInfo, error) {
	// Use system_profiler to get display info
	cmd := exec.Command("system_profiler", "SPDisplaysDataType", "-json")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: assume single display
		return []displayInfo{{ID: 1, Name: "Main Display"}}, nil
	}

	// Parse resolution from system_profiler output
	// Look for "Resolution:" lines
	displays := []displayInfo{}
	lines := strings.Split(string(output), "\n")

	// Simpler parsing: count displays and extract resolutions
	resPattern := regexp.MustCompile(`"_spdisplays_resolution"\s*:\s*"(\d+)\s*x\s*(\d+)`)
	matches := resPattern.FindAllStringSubmatch(string(output), -1)

	for i, match := range matches {
		if len(match) >= 3 {
			width, _ := strconv.Atoi(match[1])
			height, _ := strconv.Atoi(match[2])
			name := "Display"
			if i == 0 {
				name = "Main"
			} else {
				name = fmt.Sprintf("Display %d", i+1)
			}
			displays = append(displays, displayInfo{
				ID:     i + 1,
				Width:  width,
				Height: height,
				Name:   name,
			})
		}
	}

	// If no displays found via JSON, try simple grep
	if len(displays) == 0 {
		for i, line := range lines {
			if strings.Contains(line, "Resolution:") {
				// Extract resolution like "2560 x 1440"
				parts := strings.Split(line, ":")
				if len(parts) > 1 {
					resPart := strings.TrimSpace(parts[1])
					dimPattern := regexp.MustCompile(`(\d+)\s*x\s*(\d+)`)
					if m := dimPattern.FindStringSubmatch(resPart); len(m) >= 3 {
						width, _ := strconv.Atoi(m[1])
						height, _ := strconv.Atoi(m[2])
						displays = append(displays, displayInfo{
							ID:     len(displays) + 1,
							Width:  width,
							Height: height,
							Name:   fmt.Sprintf("Display %d", i+1),
						})
					}
				}
			}
		}
	}

	// Fallback if still no displays
	if len(displays) == 0 {
		displays = []displayInfo{{ID: 1, Name: "Main Display"}}
	}

	return displays, nil
}

// CaptureAll captures all screens and returns ScreenCapture structs
func CaptureAll(tempDir string) ([]*ScreenCapture, error) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	// Ensure temp directory exists
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Get display information
	displays, err := getDisplays()
	if err != nil {
		return nil, fmt.Errorf("failed to get display info: %w", err)
	}

	captures := make([]*ScreenCapture, 0, len(displays))
	timestamp := time.Now()

	// Capture each display
	for i, display := range displays {
		filename := fmt.Sprintf("screen_%d_%d.png", i+1, timestamp.UnixNano())
		path := filepath.Join(tempDir, filename)

		// Use screencapture with -D flag to specify display
		// -x: no sound
		// -t png: PNG format
		// -D <display>: display number (1-based)
		var cmd *exec.Cmd
		if len(displays) == 1 {
			// Single display: simple capture
			cmd = exec.Command("screencapture", "-x", "-t", "png", path)
		} else {
			// Multi-display: capture specific display
			cmd = exec.Command("screencapture", "-x", "-t", "png", "-D", strconv.Itoa(i+1), path)
		}

		if err := cmd.Run(); err != nil {
			// Try without -D flag as fallback
			cmd = exec.Command("screencapture", "-x", "-t", "png", path)
			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("failed to capture screen %d: %w", i+1, err)
			}
		}

		// Verify file was created
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("screenshot file not created for display %d: %w", i+1, err)
		}

		// Skip empty files (permission denied cases result in 0-byte files)
		if info.Size() == 0 {
			os.Remove(path)
			return nil, fmt.Errorf("screen capture permission denied - grant Screen Recording access in System Preferences > Privacy & Security > Screen Recording")
		}

		captures = append(captures, &ScreenCapture{
			Path:      path,
			DisplayID: display.ID,
			Width:     display.Width,
			Height:    display.Height,
			Timestamp: timestamp,
		})
	}

	return captures, nil
}

// CaptureOne captures a single screenshot of all screens combined
func CaptureOne(tempDir string) (*ScreenCapture, error) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	timestamp := time.Now()
	filename := fmt.Sprintf("screen_all_%d.png", timestamp.UnixNano())
	path := filepath.Join(tempDir, filename)

	// Capture all screens in one image (default behavior without -D)
	cmd := exec.Command("screencapture", "-x", "-t", "png", path)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to capture screen: %w", err)
	}

	// Verify file was created
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("screenshot file not created: %w", err)
	}

	if info.Size() == 0 {
		os.Remove(path)
		return nil, fmt.Errorf("screen capture permission denied - grant Screen Recording access in System Preferences > Privacy & Security > Screen Recording")
	}

	return &ScreenCapture{
		Path:      path,
		DisplayID: 0, // 0 indicates combined capture
		Width:     0, // Unknown for combined
		Height:    0,
		Timestamp: timestamp,
	}, nil
}

// Cleanup removes screenshot files
func Cleanup(captures []*ScreenCapture) error {
	var lastErr error
	for _, cap := range captures {
		if cap != nil && cap.Path != "" {
			if err := os.Remove(cap.Path); err != nil && !os.IsNotExist(err) {
				lastErr = err
			}
		}
	}
	return lastErr
}

// CleanupPath removes a single screenshot file
func CleanupPath(path string) error {
	if path == "" {
		return nil
	}
	return os.Remove(path)
}

// GetDisplayCount returns the number of connected displays
func GetDisplayCount() int {
	displays, err := getDisplays()
	if err != nil {
		return 1
	}
	return len(displays)
}

// GetDisplayInfo returns formatted display information string
func GetDisplayInfo() string {
	displays, err := getDisplays()
	if err != nil || len(displays) == 0 {
		return "1 display"
	}

	if len(displays) == 1 {
		d := displays[0]
		if d.Width > 0 && d.Height > 0 {
			return fmt.Sprintf("1 display (%s: %dx%d)", d.Name, d.Width, d.Height)
		}
		return "1 display"
	}

	// Multiple displays
	parts := make([]string, 0, len(displays))
	for _, d := range displays {
		if d.Width > 0 && d.Height > 0 {
			parts = append(parts, fmt.Sprintf("%s: %dx%d", d.Name, d.Width, d.Height))
		} else {
			parts = append(parts, d.Name)
		}
	}
	return fmt.Sprintf("%d displays (%s)", len(displays), strings.Join(parts, ", "))
}
