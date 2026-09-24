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

// fakeTerraformArgv prints each argument terraform received in brackets, so one argument "state list"
// and the two arguments state and list read differently.
const fakeTerraformArgv = "#!/bin/sh\nfor a in \"$@\"; do printf '[%s]' \"$a\"; done\necho\n"

// TestTerraformReadsAMultiWordCommandAsTerraformWould pins ruling P3-R66: a command parameter that
// holds several words (command "state list") runs as those words, the first the command and the rest
// leading the arguments, the way terraform reads them; "state list" as one argument is a command
// terraform does not have. The classifier and the dry run read the same first word, so an apply in
// the command parameter still goes through the session plan and its required dry run.
func TestTerraformReadsAMultiWordCommandAsTerraformWould(t *testing.T) {
	fakeBin(t, "terraform", fakeTerraformArgv)
	tool := TerraformTool()
	dir := t.TempDir()
	cases := []struct {
		params map[string]any
		ran    string
	}{
		{map[string]any{"command": "state list"}, "[state][list]"},
		{map[string]any{"command": "state", "args": "list"}, "[state][list]"},
		{map[string]any{"command": "state show aws_instance.web"}, "[state][show][aws_instance.web]"},
		{map[string]any{"command": "state show", "args": "aws_instance.web"}, "[state][show][aws_instance.web]"},
		{map[string]any{"command": "workspace list"}, "[workspace][list]"},
		{map[string]any{"command": "providers lock", "args": "-platform=linux_amd64"},
			"[providers][lock][-platform=linux_amd64]"},
		{map[string]any{"command": "  state   list  "}, "[state][list]"},
	}
	for _, c := range cases {
		if out, err := tool.Run(context.Background(), c.params, dir); err != nil || out != c.ran {
			t.Errorf("%v: ran %q (%v), want %q", c.params, out, err, c.ran)
		}
	}
	classes := []struct {
		params map[string]any
		verb   string
		class  policy.Classification
	}{
		{map[string]any{"command": "state list"}, "state list", policy.Read},
		{map[string]any{"command": "workspace list"}, "workspace list", policy.Read},
		{map[string]any{"command": "state rm aws_instance.web"}, "state rm", policy.Mutate},
		{map[string]any{"command": "apply -auto-approve"}, "apply", policy.Mutate},
	}
	for _, c := range classes {
		if inv := tool.Classify(c.params, dir); inv.Verb != c.verb || inv.Classification != c.class {
			t.Errorf("%v: %+v, want %s %s", c.params, inv, c.class, c.verb)
		}
	}
	apply := map[string]any{"command": "apply -auto-approve"}
	if _, err := tool.Run(context.Background(), apply, dir); err == nil || !strings.Contains(err.Error(), "terraform plan first") {
		t.Errorf("an apply in the command parameter must need this session's plan: %v", err)
	}
	if _, err := tool.DryRun(context.Background(), apply, dir); err == nil || err == ErrNoDryRun {
		t.Errorf("an apply in the command parameter must have its dry run: %v", err)
	}
}

// TestTerraformCommandSplitsTheCommandParameter covers the shared normalization the tool and the
// eval signature both call.
func TestTerraformCommandSplitsTheCommandParameter(t *testing.T) {
	command, args, err := TerraformCommand(map[string]any{"command": "state show", "args": "'aws_instance.web[\"a\"]'"})
	if err != nil || command != "state" || strings.Join(args, "|") != `show|aws_instance.web["a"]` {
		t.Errorf("%q %q %v", command, args, err)
	}
	if _, _, err := TerraformCommand(map[string]any{"command": "  "}); err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("an empty command must be an error: %v", err)
	}
	if _, _, err := TerraformCommand(map[string]any{"command": "plan", "args": "-var 'x"}); err == nil {
		t.Error("an unbalanced quote must be an error")
	}
}
