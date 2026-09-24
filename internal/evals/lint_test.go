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

// TestLintFlagsAFileSharedByTwoSignatures covers the lint-time half of ruling P3-R27: two entries
// hand-authored to point at the same file are each individually valid (a plain base name, no
// duplicate signature), so LoadFixtures accepts the index; only content-level lint can catch it.
func TestLintFlagsAFileSharedByTwoSignatures(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "shared.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexYAML := "fixtures:\n  - signature: kubectl get pod -n shop\n    file: shared.txt\n" +
		"  - signature: kubectl get pod -n other\n    file: shared.txt\n"
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "index.yaml"), []byte(indexYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	if !strings.Contains(strings.Join(problems, "\n"), "shared.txt is shared by multiple signatures") {
		t.Fatalf("%q", problems)
	}
}

// TestLintScansIndexYAMLItselfForASecret covers the index.yaml half of ruling P3-R28: the old lint
// only scanned the referenced fixture files, never the index file that names them.
func TestLintScansIndexYAMLItselfForASecret(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "a.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexYAML := "fixtures:\n  - signature: key AKIAIOSFODNN7EXAMPLE leaked in the signature itself\n    file: a.txt\n"
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "index.yaml"), []byte(indexYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	if !strings.Contains(strings.Join(problems, "\n"), "index.yaml carries a raw secret pattern") {
		t.Fatalf("%q", problems)
	}
}

// TestLintSkipsDotfilesAndCatchesWhatTheOldPatternMissed covers ruling P3-R28: the old lint.go
// rawSecret pattern missed an ASIA key, a CRLF-formatted PEM body and a github_pat_ token, and would
// have flagged a stray .DS_Store as an orphan.
func TestLintSkipsDotfilesAndCatchesWhatTheOldPatternMissed(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	s, _ := LoadFixtures(dir)
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl get secret -n shop", "key ASIAIOSFODNN7EXAMPLE leaked", false); err != nil {
		t.Fatal(err)
	}
	crlfPEM := "-----BEGIN RSA PRIVATE KEY-----\r\nMIIabc\r\n-----END RSA PRIVATE KEY-----"
	if err := s.Save("kubectl get secret -n other", crlfPEM, false); err != nil {
		t.Fatal(err)
	}
	githubPAT := "token github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz here"
	if err := s.Save("kubectl get secret -n third", githubPAT, false); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if strings.Contains(joined, "DS_Store") {
		t.Fatalf("a dotfile must not be flagged as an orphan: %q", joined)
	}
	if count := strings.Count(joined, "carries a raw secret pattern"); count != 3 {
		t.Fatalf("expected 3 raw-secret problems (ASIA key, CRLF PEM, github_pat_ token), got %d: %q", count, joined)
	}
}
