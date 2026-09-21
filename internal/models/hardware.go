package models

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// HostRAMGB returns the physical memory of this machine in gigabytes.
func HostRAMGB() (int, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0, fmt.Errorf("models: sysctl hw.memsize: %w", err)
		}
		bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("models: parse hw.memsize: %w", err)
		}
		return int(bytes / (1 << 30)), nil
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0, fmt.Errorf("models: read /proc/meminfo: %w", err)
		}
		if gb := parseMeminfo(string(data)); gb > 0 {
			return gb, nil
		}
		return 0, fmt.Errorf("models: MemTotal not found in /proc/meminfo")
	}
	return 0, fmt.Errorf("models: RAM detection not supported on %s", runtime.GOOS)
}

// parseMeminfo extracts MemTotal (kB) from /proc/meminfo text and returns whole gigabytes.
func parseMeminfo(text string) int {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return int(kb / (1 << 20))
	}
	return 0
}
