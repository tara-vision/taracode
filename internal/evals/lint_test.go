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

// TestLintScansEveryFileUnderFixturesIncludingDotfilesAndNested covers the I6 clause of ruling
// P3-R38: only indexed files and index.yaml were scanned before, so a top-level dotfile and a file
// nested under a hidden directory both lint clean; the dotfile exemption stays, but only for the
// "not in the index" orphan message.
func TestLintScansEveryFileUnderFixturesIncludingDotfilesAndNested(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	s, _ := LoadFixtures(dir)
	if err := s.Save("kubectl get pod -n shop", "ok", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", ".env"), []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fixtures", ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", ".hidden", "k.txt"), []byte("AKIAIOSFODNN7EXAMPLE"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, ".env") {
		t.Fatalf(".env must be scanned for a secret: %q", problems)
	}
	if !strings.Contains(joined, ".hidden/k.txt") {
		t.Fatalf(".hidden/k.txt must be scanned for a secret: %q", problems)
	}
	if count := strings.Count(joined, "carries a raw secret pattern"); count != 2 {
		t.Fatalf("expected 2 raw-secret problems (.env and .hidden/k.txt), got %d: %q", count, joined)
	}
	for _, p := range problems {
		if strings.Contains(p, "not in the index") && (strings.Contains(p, ".env") || strings.Contains(p, ".hidden")) {
			t.Fatalf("a dotfile must stay exempt from the orphan message: %q", p)
		}
	}
}

// TestLintFlagsShapesTheRedactorPatternsMiss covers the lint-strength half of ruling P3-R38: a
// PRIVATE KEY header with a body but no END line, an AWS key glued to a trailing word character, and
// a GitHub token glued to a leading one, all pass redact.ContainsSecret's more careful, \b-anchored
// or END-requiring patterns; the restored, narrower rawSecret regexp still catches all three.
func TestLintFlagsShapesTheRedactorPatternsMiss(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	s, _ := LoadFixtures(dir)
	if err := s.Save("kubectl get secret -n a", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAtest\n", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl get secret -n b", "key AKIAIOSFODNN7EXAMPLE_backup here", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl get secret -n c", "token xghp_"+strings.Repeat("a", 36)+" here", false); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if count := strings.Count(joined, "carries a raw secret pattern"); count != 3 {
		t.Fatalf("expected 3 raw-secret problems (headerless PEM, glued AKIA, glued ghp_), got %d: %q", count, joined)
	}
}

// TestLintFlagsASymlinkUnderWorkdir covers ruling P3-R38: a task's workdir is copied verbatim into
// every run, so a symlink there is fixture content that behaves differently across machines.
func TestLintFlagsASymlinkUnderWorkdir(t *testing.T) {
	root := t.TempDir()
	files := strings.NewReplacer("id: crashloop-oomkilled", "id: files-task", "provenance: recorded", "provenance: files").
		Replace(strings.SplitN(goodTask, "record:", 2)[0])
	dir := writeTask(t, root, "files-task", files)
	workdir := filepath.Join(dir, "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "elsewhere.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workdir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "workdir/link.txt is a symlink") {
		t.Fatalf("expected a workdir symlink problem: %q", problems)
	}
}

// TestLintFlagsAnUnreadableFixtureFile covers an addendum found alongside ruling P3-R41: the
// WalkDir callback in lintFixtureContents returned nil on an os.ReadFile failure, silently skipping
// both the secret scan and the orphan check for a file that exists but cannot be read (mode 000, or a
// dangling symlink) rather than reporting it as its own problem.
func TestLintFlagsAnUnreadableFixtureFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions")
	}
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	s, _ := LoadFixtures(dir)
	if err := s.Save("kubectl get pod -n shop", "ok", false); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "fixtures", "locked.txt")
	if err := os.WriteFile(locked, []byte("secret AKIAIOSFODNN7EXAMPLE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) }) // so t.TempDir()'s own cleanup can remove it
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "locked.txt cannot be read") {
		t.Fatalf("expected an unreadable-file problem naming locked.txt: %q", problems)
	}
	if strings.Contains(joined, "locked.txt is not in the index") {
		t.Fatalf("an unreadable file must not also be checked for the orphan message: %q", problems)
	}
	if strings.Contains(joined, "locked.txt carries a raw secret pattern") {
		t.Fatalf("an unreadable file must not also be scanned for a secret: %q", problems)
	}
}

// TestLintFlagsLeftoverRecordingArtifacts covers ruling P3-R41 item 5: a fixtures-recording-* or
// fixtures.replaced directory only exists when a recording run crashed or its final swap failed
// (record.go's RecordTask and swapFixtures), so lint must flag it rather than lint clean around it.
func TestLintFlagsLeftoverRecordingArtifacts(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	s, _ := LoadFixtures(dir)
	if err := s.Save("kubectl get pod -n shop", "ok", false); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fixtures-recording-123456"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fixtures.replaced"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "fixtures-recording-123456") || !strings.Contains(joined, "run make record again") {
		t.Fatalf("expected a leftover fixtures-recording-* problem: %q", problems)
	}
	if !strings.Contains(joined, "fixtures.replaced") {
		t.Fatalf("expected a leftover fixtures.replaced problem: %q", problems)
	}
}
