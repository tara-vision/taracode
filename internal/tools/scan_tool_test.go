package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestScanToolBuildsScannerCommands(t *testing.T) {
	for _, bin := range []string{"trivy", "gitleaks", "tfsec", "kubesec", "govulncheck"} {
		fakeBin(t, bin, "")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy.yaml"), []byte("kind: Pod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := ScanTool("HIGH,CRITICAL")
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"scanner": "trivy", "target": "nginx:1.27"}, "trivy image --no-progress --format table --severity HIGH,CRITICAL nginx:1.27"},
		{map[string]any{"scanner": "trivy", "target": ".", "severity": "LOW"}, "trivy fs --no-progress --scanners vuln,misconfig,secret --format table --severity LOW " + dir},
		{map[string]any{"scanner": "gitleaks"}, "gitleaks detect --source " + dir + " --no-banner --redact -v --exit-code 0"},
		{map[string]any{"scanner": "tfsec"}, "tfsec " + dir + " --no-color --soft-fail --minimum-severity HIGH"},
		{map[string]any{"scanner": "kubesec", "target": "deploy.yaml"}, "kubesec scan " + filepath.Join(dir, "deploy.yaml")},
		{map[string]any{"scanner": "dependency"}, "govulncheck ./..."},
	}
	for _, c := range cases {
		out, err := tool.Run(context.Background(), c.args, dir)
		if err != nil || !strings.Contains(out, c.want) {
			t.Errorf("%v: got %q (%v), want %q", c.args, out, err, c.want)
		}
	}
	if _, err := tool.Run(context.Background(), map[string]any{"scanner": "nmap"}, dir); err == nil {
		t.Error("unknown scanner must error")
	}
	if _, err := tool.Run(context.Background(), map[string]any{"scanner": "dependency"}, t.TempDir()); err == nil {
		t.Error("no manifest must error")
	}
	if inv := tool.Classify(map[string]any{"scanner": "trivy"}, dir); inv.Classification != policy.Read {
		t.Errorf("%+v", inv)
	}
}

func TestScanFindingsAreNotErrors(t *testing.T) {
	fakeBin(t, "tfsec", "#!/bin/sh\necho 'CRITICAL: open security group'\nexit 1\n")
	out, err := ScanTool("").Run(context.Background(), map[string]any{"scanner": "tfsec"}, t.TempDir())
	if err != nil || !strings.Contains(out, "open security group") || !strings.Contains(out, "findings") {
		t.Fatalf("%q %v", out, err)
	}
}

// TestScanTargetAndSeverityReadAsTheToolRunsThem pins ruling P3-R66: the working directory itself
// ("", ".", or its own path) is the default target, a path inside it is relative to it, and anything
// else (an image, a relative path, a path outside, a sibling that only shares the prefix) stays as
// given; a severity list is upper case, in rising order, once each.
func TestScanTargetAndSeverityReadAsTheToolRunsThem(t *testing.T) {
	targets := []struct{ target, want string }{
		{"", ""}, {".", ""}, {"./", ""}, {"/w/run", ""}, {"/w/run/", ""}, {"/w/run/repo", "repo"},
		{"/w/run/./repo/", "repo"}, {"/w/run/repo/sub", "repo/sub"}, {"repo", "repo"}, {"./repo", "./repo"},
		{"/w/other", "/w/other"}, {"/w/runner/x", "/w/runner/x"}, {"/w/run/../other", "/w/run/../other"},
		{"python:3.9.0-alpine", "python:3.9.0-alpine"},
	}
	for _, c := range targets {
		if got := ScanTarget(c.target, "/w/run"); got != c.want {
			t.Errorf("ScanTarget(%q) = %q, want %q", c.target, got, c.want)
		}
	}
	if got := ScanTarget("/w/run/repo", ""); got != "/w/run/repo" {
		t.Errorf("no working directory keeps an absolute target: %q", got)
	}
	severities := []struct{ severity, want string }{
		{"CRITICAL,HIGH", "HIGH,CRITICAL"}, {"high, critical", "HIGH,CRITICAL"}, {"HIGH,HIGH", "HIGH"},
		{"critical,low,MEDIUM", "LOW,MEDIUM,CRITICAL"}, {"", ""}, {"CRIT,high", "HIGH,CRIT"},
	}
	for _, c := range severities {
		if got := ScanSeverity(c.severity); got != c.want {
			t.Errorf("ScanSeverity(%q) = %q, want %q", c.severity, got, c.want)
		}
	}
}

// TestScanToolRunsEquivalentTargetsAlike: the working directory by its own path, ".", and no target
// run the same scan, and so do a path inside it and its relative form.
func TestScanToolRunsEquivalentTargetsAlike(t *testing.T) {
	fakeBin(t, "gitleaks", "")
	fakeBin(t, "trivy", "")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool := ScanTool("CRITICAL,HIGH")
	run := func(args map[string]any) string {
		out, err := tool.Run(context.Background(), args, dir)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	whole := run(map[string]any{"scanner": "gitleaks"})
	for _, target := range []string{".", dir, dir + "/"} {
		if got := run(map[string]any{"scanner": "gitleaks", "target": target}); got != whole {
			t.Errorf("target %q ran %q, want %q", target, got, whole)
		}
	}
	if a, b := run(map[string]any{"scanner": "gitleaks", "target": filepath.Join(dir, "repo")}),
		run(map[string]any{"scanner": "gitleaks", "target": "repo"}); a != b {
		t.Errorf("an absolute path inside and its relative form: %q vs %q", a, b)
	}
	if got := run(map[string]any{"scanner": "trivy", "target": "nginx:1.27"}); !strings.Contains(got, "--severity HIGH,CRITICAL nginx:1.27") {
		t.Errorf("the default severity runs in rising order: %q", got)
	}
}
