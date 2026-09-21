package models

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestRegistryLoadsAndRecommends(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) < 8 {
		t.Fatalf("registry too small: %d entries", len(r.Entries))
	}
	for _, e := range r.Entries {
		if e.Name == "" || e.DownloadGB <= 0 || e.Context <= 0 || e.Tier == "" {
			t.Fatalf("incomplete entry: %+v", e)
		}
	}
	rec := r.Recommend(32)
	if len(rec) == 0 || rec[0].Name != "qwen3.8:27b" {
		t.Fatalf("recommend(32) = %+v", rec)
	}
}

func TestRegistryDefaultForTier(t *testing.T) {
	r, _ := Load()
	if got := r.DefaultForTier(Tier16).Name; got != "gemma4:12b" {
		t.Fatalf("16 GB default = %q", got)
	}
	if got := r.DefaultForTier(Tier32).Name; got != "qwen3.8:27b" {
		t.Fatalf("32 GB default = %q", got)
	}
	if got := r.DefaultForTier(Tier48).Name; got != "qwen3.6:35b" {
		t.Fatalf("48 GB default = %q", got)
	}
}

func TestTierForBoundaries(t *testing.T) {
	if TierFor(8) != Tier16 || TierFor(24) != Tier16 || TierFor(32) != Tier32 || TierFor(36) != Tier32 || TierFor(48) != Tier48 || TierFor(128) != Tier48 {
		t.Fatal("tier boundaries")
	}
}

func TestRegistryFind(t *testing.T) {
	r, _ := Load()
	e, ok := r.Find("qwen3.8")
	if !ok || e.Name != "qwen3.8:27b" || e.MinOllama != "0.32.12" {
		t.Fatalf("find by family = %+v %v", e, ok)
	}
	if _, ok := r.Find("nope"); ok {
		t.Fatal("unknown model must not be found")
	}
}

func TestSmallEntryIsMarked(t *testing.T) {
	r, _ := Load()
	e, ok := r.Find("gemma4:e4b")
	if !ok || e.Tier != TierSmall {
		t.Fatalf("small entry: %+v %v", e, ok)
	}
}

func TestDefaultForTierFallsBackToFirstEntry(t *testing.T) {
	r, _ := Load()
	// TierSmall has exactly one entry and it carries no default: true, so
	// DefaultForTier must fall back to returning that entry rather than a zero value.
	e := r.DefaultForTier(TierSmall)
	if e.Name != "gemma4:e4b" {
		t.Fatalf("small default fallback = %+v", e)
	}
}

func TestDefaultForTierUnknownTierReturnsZeroValue(t *testing.T) {
	r, _ := Load()
	e := r.DefaultForTier(Tier("unknown"))
	if e.Name != "" || e.Tier != "" || e.Capabilities != nil {
		t.Fatalf("unknown tier default = %+v, want zero value", e)
	}
}

func TestDefaultNameResolvesViaFind(t *testing.T) {
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	name := DefaultName(Tier32)
	if name == "" {
		t.Fatal("DefaultName(Tier32) is empty")
	}
	if _, ok := r.Find(name); !ok {
		t.Fatalf("Find cannot resolve DefaultName(Tier32) = %q", name)
	}
	if got := DefaultName(Tier16); got == name {
		t.Fatalf("Tier16 and Tier32 defaults must differ, both = %q", got)
	}
}

// TestNoModelLiteralsOutsideTheRegistry is the repo-wide guard: every Ollama model name in Go code
// must come from this registry (embedded in registry.yaml), not from a string literal. It excludes
// tests (which pin exact names on purpose) and this package (the registry itself).
func TestNoModelLiteralsOutsideTheRegistry(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not inside a git checkout: %v", err)
	}
	cmd := exec.Command("git", "grep", "-n", "-E",
		`(gemma4|qwen3\.[568]|glm-4\.7|muse-glimmer|nemotron|ministral)`,
		"--", "*.go", ":!*_test.go", ":!internal/models/")
	cmd.Dir = strings.TrimSpace(string(root))
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() != 1 {
		t.Fatalf("git grep failed: %v", err) // exit 1 is "no match", the pass case
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Fatalf("model names must live in the registry only:\n%s", out)
	}
}
