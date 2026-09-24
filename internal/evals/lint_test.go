package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintFindsMissingPolicyAndWorkdir(t *testing.T) {
	root := t.TempDir()
	operate := strings.NewReplacer("id: crashloop-oomkilled", "id: op-task", "mode: investigate", "mode: operate",
		"prompt:", "policy: policy.yaml\nprompt:").Replace(goodTask)
	writeTask(t, root, "op-task", operate)
	files := strings.NewReplacer("id: crashloop-oomkilled", "id: files-task", "provenance: recorded", "provenance: files").
		Replace(strings.SplitN(goodTask, "record:", 2)[0])
	writeTask(t, root, "files-task", files)
	count, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if count != 2 || !strings.Contains(joined, "op-task: policy file policy.yaml missing") ||
		!strings.Contains(joined, "files-task: a files task needs a workdir") {
		t.Fatalf("count=%d problems=%q", count, joined)
	}
	if err := os.WriteFile(filepath.Join(root, "op-task", "policy.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "files-task", "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Task 10 adds the fixture-index lint; until then, fixing the policy file and the workdir
	// leaves both tasks clean.
	if _, problems := Lint(root); len(problems) != 0 {
		t.Fatalf("after fixes: %q", problems)
	}
}
