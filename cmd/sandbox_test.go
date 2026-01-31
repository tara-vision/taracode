package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxedPath(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir, err := os.MkdirTemp("", "sandbox-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectories
	subDirs := []string{"src", "src/internal", "src/internal/tools", "pkg", "docs"}
	for _, dir := range subDirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatalf("failed to create subdir %s: %v", dir, err)
		}
	}

	tests := []struct {
		name          string
		target        string
		currentRelDir string
		projectRoot   string
		wantRelDir    string
		wantErr       bool
		errContains   string
	}{
		{
			name:          "cd to subdirectory from root",
			target:        "src",
			currentRelDir: "",
			projectRoot:   tmpDir,
			wantRelDir:    "src",
			wantErr:       false,
		},
		{
			name:          "cd to nested subdirectory",
			target:        "internal/tools",
			currentRelDir: "src",
			projectRoot:   tmpDir,
			wantRelDir:    "src/internal/tools",
			wantErr:       false,
		},
		{
			name:          "cd .. from subdirectory",
			target:        "..",
			currentRelDir: "src/internal",
			projectRoot:   tmpDir,
			wantRelDir:    "src",
			wantErr:       false,
		},
		{
			name:          "cd .. blocked at root",
			target:        "..",
			currentRelDir: "",
			projectRoot:   tmpDir,
			wantErr:       true,
			errContains:   "cannot navigate above project root",
		},
		{
			name:          "cd ../.. blocked when it escapes",
			target:        "../..",
			currentRelDir: "src",
			projectRoot:   tmpDir,
			wantErr:       true,
			errContains:   "cannot navigate above project root",
		},
		{
			name:          "absolute path blocked",
			target:        "/etc",
			currentRelDir: "",
			projectRoot:   tmpDir,
			wantErr:       true,
			errContains:   "absolute paths not allowed",
		},
		{
			name:          "cd with no target returns to root",
			target:        "",
			currentRelDir: "src/internal",
			projectRoot:   tmpDir,
			wantRelDir:    "",
			wantErr:       false,
		},
		{
			name:          "cd to non-existent directory",
			target:        "nonexistent",
			currentRelDir: "",
			projectRoot:   tmpDir,
			wantErr:       true,
			errContains:   "directory does not exist",
		},
		{
			name:          "cd . stays in current",
			target:        ".",
			currentRelDir: "src",
			projectRoot:   tmpDir,
			wantRelDir:    "src",
			wantErr:       false,
		},
		{
			name:          "cd to sibling directory",
			target:        "../pkg",
			currentRelDir: "src",
			projectRoot:   tmpDir,
			wantRelDir:    "pkg",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRel, gotAbs, err := SandboxedPath(tt.target, tt.currentRelDir, tt.projectRoot)

			if tt.wantErr {
				if err == nil {
					t.Errorf("SandboxedPath() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("SandboxedPath() error = %q, want error containing %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("SandboxedPath() unexpected error: %v", err)
				return
			}

			if gotRel != tt.wantRelDir {
				t.Errorf("SandboxedPath() relDir = %q, want %q", gotRel, tt.wantRelDir)
			}

			// Verify absolute path is correct
			expectedAbs := tt.projectRoot
			if tt.wantRelDir != "" {
				expectedAbs = filepath.Join(tt.projectRoot, tt.wantRelDir)
			}
			if gotAbs != expectedAbs {
				t.Errorf("SandboxedPath() absDir = %q, want %q", gotAbs, expectedAbs)
			}
		})
	}
}

func TestSandboxedPathWithFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file (not a directory)
	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, _, err = SandboxedPath("test.txt", "", tmpDir)
	if err == nil {
		t.Error("SandboxedPath() expected error for file path, got nil")
	}
	if !contains(err.Error(), "not a directory") {
		t.Errorf("SandboxedPath() error = %q, want error containing 'not a directory'", err.Error())
	}
}

func TestFormatPrompt(t *testing.T) {
	tests := []struct {
		name   string
		relDir string
		want   string
	}{
		{
			name:   "at project root",
			relDir: "",
			want:   "\033[34m❯\033[0m ",
		},
		{
			name:   "in subdirectory",
			relDir: "src",
			want:   "\033[90m[src]\033[0m \033[34m❯\033[0m ",
		},
		{
			name:   "in nested directory",
			relDir: "src/internal",
			want:   "\033[90m[src/internal]\033[0m \033[34m❯\033[0m ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPrompt(tt.relDir)
			if got != tt.want {
				t.Errorf("FormatPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatPromptWithContext(t *testing.T) {
	tests := []struct {
		name       string
		relDir     string
		usedTokens int
		maxTokens  int
		wantParts  []string // Parts that should appear in the prompt
	}{
		{
			name:       "no context tracking",
			relDir:     "",
			usedTokens: 0,
			maxTokens:  0,
			wantParts:  []string{"\033[34m❯\033[0m "},
		},
		{
			name:       "with context tracking at root",
			relDir:     "",
			usedTokens: 5000,
			maxTokens:  32000,
			wantParts:  []string{"5.0k/32k", "\033[36m"}, // Cyan for normal usage
		},
		{
			name:       "with directory and context",
			relDir:     "src",
			usedTokens: 10000,
			maxTokens:  32000,
			wantParts:  []string{"[src]", "10k/32k"},
		},
		{
			name:       "high usage warning (75%)",
			relDir:     "",
			usedTokens: 24000,
			maxTokens:  32000,
			wantParts:  []string{"\033[33m"}, // Yellow for warning (75%)
		},
		{
			name:       "critical usage (90%)",
			relDir:     "",
			usedTokens: 29000,
			maxTokens:  32000,
			wantParts:  []string{"\033[31m"}, // Red for critical (>90%)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPromptWithContext(tt.relDir, tt.usedTokens, tt.maxTokens)
			for _, part := range tt.wantParts {
				if !contains(got, part) {
					t.Errorf("FormatPromptWithContext() = %q, missing expected part %q", got, part)
				}
			}
		})
	}
}

func TestFormatPromptWithMode(t *testing.T) {
	tests := []struct {
		name         string
		relDir       string
		usedTokens   int
		maxTokens    int
		securityMode bool
		wantParts    []string
		dontWant     []string
	}{
		{
			name:         "devops mode at root",
			relDir:       "",
			usedTokens:   0,
			maxTokens:    0,
			securityMode: false,
			wantParts:    []string{"\033[34m❯\033[0m "},
			dontWant:     []string{"🛡"},
		},
		{
			name:         "security mode at root",
			relDir:       "",
			usedTokens:   0,
			maxTokens:    0,
			securityMode: true,
			wantParts:    []string{"🛡", "\033[34m❯\033[0m "},
		},
		{
			name:         "security mode with directory",
			relDir:       "src",
			usedTokens:   0,
			maxTokens:    0,
			securityMode: true,
			wantParts:    []string{"🛡", "[src]"},
		},
		{
			name:         "security mode with context",
			relDir:       "",
			usedTokens:   5000,
			maxTokens:    32000,
			securityMode: true,
			wantParts:    []string{"🛡", "5.0k/32k"},
		},
		{
			name:         "security mode with all elements",
			relDir:       "internal/tools",
			usedTokens:   10000,
			maxTokens:    32000,
			securityMode: true,
			wantParts:    []string{"🛡", "tools]", "10k/32k"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPromptWithMode(tt.relDir, tt.usedTokens, tt.maxTokens, tt.securityMode)
			for _, part := range tt.wantParts {
				if !contains(got, part) {
					t.Errorf("FormatPromptWithMode() = %q, missing expected part %q", got, part)
				}
			}
			for _, part := range tt.dontWant {
				if contains(got, part) {
					t.Errorf("FormatPromptWithMode() = %q, should not contain %q", got, part)
				}
			}
		})
	}
}

func TestFormatPwd(t *testing.T) {
	projectRoot := "/home/user/project"

	tests := []struct {
		name        string
		relDir      string
		projectRoot string
		wantPrefix  string
	}{
		{
			name:        "at project root",
			relDir:      "",
			projectRoot: projectRoot,
			wantPrefix:  "/ (project root:",
		},
		{
			name:        "in subdirectory",
			relDir:      "src",
			projectRoot: projectRoot,
			wantPrefix:  "/src (absolute:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPwd(tt.relDir, tt.projectRoot)
			if !contains(got, tt.wantPrefix) {
				t.Errorf("FormatPwd() = %q, want prefix %q", got, tt.wantPrefix)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
