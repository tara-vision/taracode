package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestArgHelpersReadStrings(t *testing.T) {
	args := map[string]any{"n": " 7 ", "bad": "seven", "yes": " TRUE ", "no": "nope", "num": 1.0}
	if argInt(args, "n", 3) != 7 || argInt(args, "bad", 3) != 3 {
		t.Fatal("argInt")
	}
	if !argBool(args, "yes") || argBool(args, "no") || argBool(args, "num") {
		t.Fatal("argBool")
	}
}

// TestUnparsableArgsAreMutations: a command line the tools cannot split is classified as a
// mutation, with the parse error as the reason, and never runs.
func TestUnparsableArgsAreMutations(t *testing.T) {
	tests := []struct {
		tool *Tool
		args map[string]any
	}{
		{CloudTool(), map[string]any{"provider": "aws", "args": "s3 ls 'x"}},
		{DockerTool(), map[string]any{"args": "ps 'x"}},
		{GitTool(), map[string]any{"args": "log 'x"}},
		{HelmTool(), map[string]any{"args": "list 'x"}},
		{HelmTool(), map[string]any{}},
		{TerraformTool(), map[string]any{"command": "plan 'x"}},
	}
	for _, tt := range tests {
		inv := tt.tool.Classify(tt.args, t.TempDir())
		if inv.Classification != policy.Mutate || inv.Reason == "" {
			t.Errorf("%s %v: %+v", tt.tool.Name, tt.args, inv)
		}
		if _, err := tt.tool.Run(context.Background(), tt.args, t.TempDir()); err == nil {
			t.Errorf("%s %v ran", tt.tool.Name, tt.args)
		}
	}
}

func TestCommandToolsNeedTheirArguments(t *testing.T) {
	tests := []struct {
		tool *Tool
		args map[string]any
		want string
	}{
		{CloudTool(), map[string]any{"args": "s3 ls"}, "provider is required"},
		{CloudTool(), map[string]any{"provider": "aws"}, "args is required"},
		{DockerTool(), map[string]any{}, "args is required"},
		{GitTool(), map[string]any{}, "args is required"},
		{HelmTool(), map[string]any{}, "args is required"},
		{TerraformTool(), map[string]any{"command": " "}, "command is required"},
		{TerraformTool(), map[string]any{"command": "'plan"}, "unbalanced quote"},
		{TerraformTool(), map[string]any{"command": "plan", "args": "-var 'x"}, "unbalanced quote"},
		{ScanTool(""), map[string]any{}, "scanner is required"},
		{ScanTool(""), map[string]any{"scanner": "kubesec"}, "kubesec needs a manifest file as target"},
	}
	for _, tt := range tests {
		if _, err := tt.tool.Run(context.Background(), tt.args, t.TempDir()); err == nil || err.Error() != tt.want {
			t.Errorf("%s %v: %v, want %q", tt.tool.Name, tt.args, err, tt.want)
		}
	}
}

func TestDryRunsOfCallsTheyCannotRead(t *testing.T) {
	if _, err := HelmTool().DryRun(context.Background(), map[string]any{"args": "upgrade 'x"}, ""); err == nil ||
		err.Error() != "unbalanced quote" {
		t.Fatalf("helm: %v", err)
	}
	if _, err := KubectlTool().DryRun(context.Background(), map[string]any{"args": "-f x.yaml"}, ""); err == nil ||
		err.Error() != "verb is required" {
		t.Fatalf("kubectl: %v", err)
	}
	if _, err := TerraformTool().DryRun(context.Background(), map[string]any{"command": "plan"}, ""); !errors.Is(err, ErrNoDryRun) {
		t.Fatalf("terraform plan: %v", err)
	}
}

// TestKubectlDiffDryRunWithoutDifferences: kubectl diff exiting 0 with no output means the cluster
// already matches; with output, the output is the dry run.
func TestKubectlDiffDryRunWithoutDifferences(t *testing.T) {
	fakeBin(t, "kubectl", "#!/bin/sh\n[ -n \"$DIFF_SAYS\" ] && echo \"$DIFF_SAYS\"\nexit 0\n")
	tool := KubectlTool()
	apply := map[string]any{"verb": "apply", "args": "-f x.yaml"}
	if out, err := tool.DryRun(context.Background(), apply, ""); err != nil ||
		out != "kubectl diff: no differences (the cluster already matches)" {
		t.Fatalf("%q %v", out, err)
	}
	t.Setenv("DIFF_SAYS", "deployment.apps/web configured")
	if out, err := tool.DryRun(context.Background(), apply, ""); err != nil || out != "deployment.apps/web configured" {
		t.Fatalf("%q %v", out, err)
	}
}

// TestKubectlArgsKeepATypeNameForAnotherObject: a type/name in args that names another object than
// the parameters is left for kubectl to refuse.
func TestKubectlArgsKeepATypeNameForAnotherObject(t *testing.T) {
	argv, err := KubectlArgv(map[string]any{"verb": "get", "resource": "deploy", "name": "web", "args": "deploy/api"})
	if err != nil || strings.Join(argv, " ") != "get deploy web deploy/api" {
		t.Fatalf("%q %v", argv, err)
	}
}

func TestShellEdges(t *testing.T) {
	ctx, cancel := withTimeout(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("a zero timeout keeps the parent's (absent) deadline")
	}
	tool := ShellTool(nil)
	if out, err := tool.Run(context.Background(), map[string]any{"command": "true"}, t.TempDir()); err != nil || out != "(no output)" {
		t.Fatalf("%q %v", out, err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := tool.Run(cancelled, map[string]any{"command": "echo never"}, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled call: %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	tests := []struct {
		p, home, want string
	}{
		{"~/x", "", "~/x"}, {"~", "/home/u", "/home/u"}, {"$HOME", "/home/u", "/home/u"}, {"${HOME}/a", "/home/u", "/home/u/a"},
	}
	for _, tt := range tests {
		if got := expandHome(tt.p, tt.home); got != tt.want {
			t.Errorf("expandHome(%q, %q) = %q, want %q", tt.p, tt.home, got, tt.want)
		}
	}
}

// TestDependencyAuditPicksTheManifestsAuditor: each manifest runs its ecosystem's auditor in the
// target directory.
func TestDependencyAuditPicksTheManifestsAuditor(t *testing.T) {
	for _, name := range []string{"npm", "cargo", "pip-audit", "composer"} {
		fakeBin(t, name, "")
	}
	tests := map[string]string{
		"package.json": "npm audit", "Cargo.toml": "cargo audit", "requirements.txt": "pip-audit -r requirements.txt",
		"pyproject.toml": "pip-audit", "composer.json": "composer audit",
	}
	tool := ScanTool("")
	for manifest, want := range tests {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, manifest), "{}", 0o644)
		out, err := tool.Run(context.Background(), map[string]any{"scanner": "dependency", "target": dir}, dir)
		if err != nil || strings.TrimSpace(out) != want {
			t.Errorf("%s: %q %v, want %q", manifest, out, err, want)
		}
	}
	if got := lowestSeverity("bogus,other"); got != "bogus,other" {
		t.Errorf("no known level: %q", got)
	}
}

const fakeTerraformSteps = `#!/bin/sh
case "$1" in
plan) [ "$TF_PLAN_FAILS" = "1" ] && { echo "Error: no configuration"; exit 1; }; echo planned ;;
show) echo "$TF_SHOW_SAYS" ;;
*) for a in "$@"; do printf '[%s]' "$a"; done; echo ;;
esac
`

func TestTerraformRunEdges(t *testing.T) {
	fakeBin(t, "terraform", fakeTerraformSteps)
	tool := TerraformTool()
	dir := t.TempDir()
	if out, err := tool.Run(context.Background(), map[string]any{"command": "validate"}, dir); err != nil || out != "[validate][-no-color]" {
		t.Fatalf("validate: %q %v", out, err)
	}
	t.Setenv("TF_PLAN_FAILS", "1")
	if out, err := tool.Run(context.Background(), map[string]any{"command": "plan"}, dir); err == nil ||
		out != "Error: no configuration" {
		t.Fatalf("a failed plan: %q %v", out, err)
	}
	t.Setenv("TF_PLAN_FAILS", "0")
	t.Setenv("TF_SHOW_SAYS", "not json")
	if _, err := tool.Run(context.Background(), map[string]any{"command": "plan"}, dir); err == nil {
		t.Fatal("a plan terraform show cannot describe")
	}
}

func TestTerraformPlanKeepsOnlyTheNewestPlanFile(t *testing.T) {
	fakeBin(t, "terraform", fakeTerraform)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	tool := TerraformTool()
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, err := tool.Run(context.Background(), map[string]any{"command": "plan"}, dir); err != nil {
			t.Fatal(err)
		}
	}
	if plans, _ := filepath.Glob(filepath.Join(tmp, "taracode-*.tfplan")); len(plans) != 1 {
		t.Fatalf("plan files %v", plans)
	}
}

func TestTerraformPlanNeedsATemporaryFile(t *testing.T) {
	fakeBin(t, "terraform", fakeTerraform)
	file := filepath.Join(t.TempDir(), "not-a-directory")
	writeTestFile(t, file, "x", 0o644)
	t.Setenv("TMPDIR", file)
	if _, err := TerraformTool().Run(context.Background(), map[string]any{"command": "plan"}, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v", err)
	}
}
