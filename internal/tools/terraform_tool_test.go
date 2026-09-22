package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

const fakeTerraform = `#!/bin/sh
if [ "$1" = "show" ]; then
  if [ "$TARACODE_TEST_SHOW_FAIL" = "1" ]; then
    echo "terraform show failed" >&2
    exit 1
  fi
  echo '{"format_version":"1.2","terraform_version":"1.9.5","resource_changes":[{"address":"aws_instance.web","type":"aws_instance","change":{"actions":["create"]}}]}'
  exit 0
fi
echo "terraform $@"
`

//nolint:gocyclo // walks one session end to end: plan before apply, plan, dry run, apply, single-use, classify, state
func TestTerraformPlanThenApplyUsesTheSessionPlanFile(t *testing.T) {
	fakeBin(t, "terraform", fakeTerraform)
	tool := TerraformTool()
	dir := t.TempDir()
	if _, err := tool.DryRun(context.Background(), map[string]any{"command": "apply", "dir": "."}, dir); err == nil {
		t.Fatal("apply before plan must error")
	}
	if _, err := tool.Run(context.Background(), map[string]any{"command": "apply"}, dir); err == nil || !strings.Contains(err.Error(), "terraform plan first") {
		t.Fatalf("apply before plan: %v", err)
	}
	out, err := tool.Run(context.Background(), map[string]any{"command": "plan", "args": "-var-file=dev.tfvars"}, dir)
	if err != nil || !strings.Contains(out, "1 to add") || !strings.Contains(out, "aws_instance.web") {
		t.Fatalf("plan %q %v", out, err)
	}
	dry, err := tool.DryRun(context.Background(), map[string]any{"command": "apply"}, dir)
	if err != nil || !strings.Contains(dry, "1 to add") {
		t.Fatalf("dry run %q %v", dry, err)
	}
	out, err = tool.Run(context.Background(), map[string]any{"command": "apply"}, dir)
	if err != nil || !strings.Contains(out, "terraform apply -input=false -no-color") || !strings.Contains(out, ".tfplan") {
		t.Fatalf("apply %q %v", out, err)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"command": "apply"}, dir); err == nil {
		t.Fatal("a plan file is single-use")
	}
	inv := tool.Classify(map[string]any{"command": "destroy", "dir": "infra"}, dir)
	if inv.Classification != policy.Mutate || inv.Verb != "destroy" || len(inv.Targets.Paths) != 1 || !strings.HasSuffix(inv.Targets.Paths[0], "/infra") || inv.Command != "terraform destroy" {
		t.Errorf("%+v", inv)
	}
	if inv := tool.Classify(map[string]any{"command": "plan"}, dir); inv.Classification != policy.Read {
		t.Errorf("%+v", inv)
	}
	out, err = tool.Run(context.Background(), map[string]any{"command": "state", "args": "list"}, dir)
	if err != nil || !strings.Contains(out, "terraform state list") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestTerraformPlanRemovesTheFileWhenShowFails(t *testing.T) {
	fakeBin(t, "terraform", fakeTerraform)
	t.Setenv("TARACODE_TEST_SHOW_FAIL", "1")
	pattern := filepath.Join(os.TempDir(), "taracode-*.tfplan")
	before, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	existed := map[string]bool{}
	for _, p := range before {
		existed[p] = true
	}
	tool := TerraformTool()
	if _, err := tool.Run(context.Background(), map[string]any{"command": "plan"}, t.TempDir()); err == nil {
		t.Fatal("plan must error when terraform show fails")
	}
	after, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range after {
		if !existed[p] {
			t.Fatalf("plan file leaked: %s (before %v, after %v)", p, before, after)
		}
	}
}
