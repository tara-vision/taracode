package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSaveLookupAndFileNames(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadFixtures(dir)
	if err != nil || s.Len() != 0 {
		t.Fatalf("empty store: %v %d", err, s.Len())
	}
	sig := "kubectl get pod -n shop"
	if err := s.Save(sig, "NAME READY\ncheckout-1 0/1\n", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl logs checkout-1 -n shop", "kubectl exited with status 1\nno logs", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sig, "NAME READY\ncheckout-1 1/1\n", false); err != nil { // replace
		t.Fatal(err)
	}
	reloaded, err := LoadFixtures(dir)
	if err != nil || reloaded.Len() != 2 {
		t.Fatalf("reload: %v %d", err, reloaded.Len())
	}
	out, isErr, ok := reloaded.Lookup(sig)
	if !ok || isErr || !strings.Contains(out, "1/1") {
		t.Fatalf("lookup %q %v %v", out, isErr, ok)
	}
	if _, isErr, ok := reloaded.Lookup("kubectl logs checkout-1 -n shop"); !ok || !isErr {
		t.Fatal("error fixture")
	}
	if _, _, ok := reloaded.Lookup("nope"); ok {
		t.Fatal("unknown signature found")
	}
	name := fixtureFileName(sig)
	if !strings.HasPrefix(name, "kubectl-get-pod-n-shop-") || !strings.HasSuffix(name, ".txt") || len(name) > 96 {
		t.Fatalf("file name %q", name)
	}
	long := fixtureFileName("shell " + strings.Repeat("x", 200))
	if len(long) > 92 {
		t.Fatalf("long name %d", len(long))
	}
	files, _ := os.ReadDir(filepath.Join(dir, "fixtures"))
	if len(files) != 3 { // two fixtures plus index.yaml
		t.Fatalf("%d files", len(files))
	}
}

// TestLoadFixturesRejectsAnUnsafeFileNameOrADuplicateSignature covers ruling P3-R27: an index entry
// whose file field is not a plain base name could read or delete outside the fixtures directory
// (Lookup and Save's replace-time cleanup both trusted it blindly), and a duplicated signature would
// let the later entry silently shadow the earlier one. Both must fail loudly, naming index.yaml.
func TestLoadFixturesRejectsAnUnsafeFileNameOrADuplicateSignature(t *testing.T) {
	cases := map[string]string{
		"path traversal":      "fixtures:\n  - signature: a\n    file: ../../outside.txt\n",
		"nested path":         "fixtures:\n  - signature: a\n    file: sub/x.txt\n",
		"dot":                 "fixtures:\n  - signature: a\n    file: \".\"\n",
		"dotdot":              "fixtures:\n  - signature: a\n    file: \"..\"\n",
		"empty":               "fixtures:\n  - signature: a\n    file: \"\"\n",
		"duplicate signature": "fixtures:\n  - signature: a\n    file: a.txt\n  - signature: a\n    file: b.txt\n",
	}
	for name, yamlText := range cases {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fixtures", "index.yaml"), []byte(yamlText), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFixtures(dir); err == nil || !strings.Contains(err.Error(), "index.yaml") {
			t.Errorf("%s: expected an index.yaml error, got %v", name, err)
		}
	}
}

// TestSaveRefusesToReuseAFileNameFromADifferentSignature covers the write-time half of ruling
// P3-R27: Save must never overwrite a file another signature already owns.
func TestSaveRefusesToReuseAFileNameFromADifferentSignature(t *testing.T) {
	dir := t.TempDir()
	const victimSig = "kubectl get pod -n shop"
	name := fixtureFileName(victimSig)
	indexYAML := "fixtures:\n  - signature: an authored entry\n    file: " + name + "\n"
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "index.yaml"), []byte(indexYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", name), []byte("authored content"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(victimSig, "new content", false); err == nil {
		t.Fatal("expected Save to refuse a file name already used by a different signature")
	}
	out, _, ok := s.Lookup("an authored entry")
	if !ok || out != "authored content" {
		t.Fatalf("the authored fixture must survive the refused save: %q %v", out, ok)
	}
}

// TestStoreLookupErrDistinguishesACorpusDefectFromAMiss covers the last part of ruling P3-R28: an
// indexed-but-unreadable fixture file is a distinct, sticky-per-signature error, not the same
// ok=false a plain miss returns.
func TestStoreLookupErrDistinguishesACorpusDefectFromAMiss(t *testing.T) {
	dir := t.TempDir()
	s, _ := LoadFixtures(dir)
	const sig = "kubectl get pod -n shop"
	if err := s.Save(sig, "ok", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.Dir(), s.Fixtures()[0].File)); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Lookup(sig); ok {
		t.Fatal("expected the lookup to fail once the file is gone")
	}
	if s.LookupErr(sig) == nil {
		t.Fatal("expected a corpus-defect error for the indexed-but-missing file")
	}
	if _, _, ok := s.Lookup("never recorded"); ok {
		t.Fatal("expected a plain miss")
	}
	if s.LookupErr("never recorded") != nil {
		t.Fatalf("a plain miss must not report a lookup error: %v", s.LookupErr("never recorded"))
	}
}
