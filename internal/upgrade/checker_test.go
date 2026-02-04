package upgrade

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected [3]int
	}{
		{"1.0.0", [3]int{1, 0, 0}},
		{"1.2.3", [3]int{1, 2, 3}},
		{"0.1.0", [3]int{0, 1, 0}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"1.0.0-beta", [3]int{1, 0, 0}},
		{"2.1.0-rc1", [3]int{2, 1, 0}},
		{"1", [3]int{1, 0, 0}},
		{"1.2", [3]int{1, 2, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseVersion(tt.input)
			if result != tt.expected {
				t.Errorf("parseVersion(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name     string
		latest   string
		current  string
		expected bool
	}{
		{"newer major", "2.0.0", "1.0.0", true},
		{"newer minor", "1.1.0", "1.0.0", true},
		{"newer patch", "1.0.1", "1.0.0", true},
		{"same version", "1.0.0", "1.0.0", false},
		{"older major", "1.0.0", "2.0.0", false},
		{"older minor", "1.0.0", "1.1.0", false},
		{"older patch", "1.0.0", "1.0.1", false},
		{"dev version", "1.0.0", "dev", true},
		{"empty current", "1.0.0", "", true},
		{"complex newer", "1.2.3", "1.2.2", true},
		{"complex same", "1.2.3", "1.2.3", false},
		{"complex older", "1.2.2", "1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNewerVersion(tt.latest, tt.current)
			if result != tt.expected {
				t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.latest, tt.current, result, tt.expected)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{"v1 greater major", "v2.0.0", "v1.0.0", 1},
		{"v1 greater minor", "v1.1.0", "v1.0.0", 1},
		{"v1 greater patch", "v1.0.1", "v1.0.0", 1},
		{"equal versions", "v1.0.0", "v1.0.0", 0},
		{"equal without v", "1.0.0", "1.0.0", 0},
		{"v1 less major", "v1.0.0", "v2.0.0", -1},
		{"v1 less minor", "v1.0.0", "v1.1.0", -1},
		{"v1 less patch", "v1.0.0", "v1.0.1", -1},
		{"mixed v prefix", "v1.0.0", "1.0.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareVersions(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("CompareVersions(%q, %q) = %v, want %v", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

func TestGetUpgradeCommand(t *testing.T) {
	tests := []struct {
		method   InstallMethod
		expected string
	}{
		{InstallMethodHomebrew, "brew upgrade taracode"},
		{InstallMethodGo, "go install github.com/tara-vision/taracode@latest"},
		{InstallMethodCurl, "curl -fsSL https://code.tara.vision/install.sh | bash"},
		{InstallMethodManual, "curl -fsSL https://code.tara.vision/install.sh | bash"},
		{InstallMethodUnknown, "curl -fsSL https://code.tara.vision/install.sh | bash"},
	}

	for _, tt := range tests {
		t.Run(string(tt.method), func(t *testing.T) {
			result := GetUpgradeCommand(tt.method)
			if result != tt.expected {
				t.Errorf("GetUpgradeCommand(%q) = %q, want %q", tt.method, result, tt.expected)
			}
		})
	}
}
