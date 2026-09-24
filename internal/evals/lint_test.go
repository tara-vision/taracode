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
	// op-task is provenance recorded with no fixtures yet, so it still lints dirty; files-task has
	// no fixtures of its own to check and lints clean.
	if _, problems := Lint(root); len(problems) != 1 || !strings.Contains(problems[0], "op-task: fixtures index is empty") {
		t.Fatalf("after fixes: %q", problems)
	}
}

func TestLintChecksFixturesAndRawSecrets(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	if _, problems := Lint(root); len(problems) != 1 || !strings.Contains(problems[0], "fixtures index is empty") {
		t.Fatalf("empty: %q", problems)
	}
	s, _ := LoadFixtures(dir)
	if err := s.Save("kubectl get pod -n shop", "ok", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "orphan.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl get secret -n shop", "key AKIAIOSFODNN7EXAMPLE leaked", false); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "orphan.txt is not in the index") || !strings.Contains(joined, "carries a raw secret pattern") {
		t.Fatalf("%q", joined)
	}
}
