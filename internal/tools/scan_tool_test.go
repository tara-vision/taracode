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
