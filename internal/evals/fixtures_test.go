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
