package models

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	if len(rec) == 0 || rec[0].Name != "glm-4.7-flash" {
		t.Fatalf("recommend(32) = %+v", rec)
	}
}

func TestRegistryDefaultForTier(t *testing.T) {
	r, _ := Load()
	if got := r.DefaultForTier(Tier16).Name; got != "gemma4:12b" {
		t.Fatalf("16 GB default = %q", got)
	}
	if got := r.DefaultForTier(Tier32).Name; got != "glm-4.7-flash" {
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

// modelLiteralPattern matches the model-name families that must live only in the registry.
var modelLiteralPattern = regexp.MustCompile(`(gemma4|qwen3\.[568]|glm-4\.7|muse-glimmer|nemotron|ministral)`)

// moduleRoot returns the directory containing go.mod, walking up from the current working directory.
// go test runs with the package directory as its working directory, so this finds the repository
// root regardless of which package the test runs from.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

// TestNoModelLiteralsOutsideTheRegistry is the repo-wide guard: every Ollama model name in Go code
// must come from this registry (embedded in registry.yaml), not from a string literal. It excludes
// tests (which pin exact names on purpose), this package (the registry itself), .git and vendor.
//
// This walks the module tree in pure Go rather than shelling out to git grep: a subprocess is
// invisible to the Go test cache, so a change to a file the old exec-based version policed (but that
// this package's test inputs do not otherwise depend on) could leave go test reporting a stale
// (cached) PASS. Reading every file directly makes each one a real input to this test, so the cache
// is invalidated whenever any of them changes.
func TestNoModelLiteralsOutsideTheRegistry(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}

	var hits []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch rel {
			case ".git", "internal/models", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if modelLiteralPattern.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d:%s", rel, i+1, line))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk module tree: %v", walkErr)
	}
	if len(hits) > 0 {
		t.Fatalf("model names must live in the registry only:\n%s", strings.Join(hits, "\n"))
	}
}
