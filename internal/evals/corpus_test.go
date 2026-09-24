package evals

import (
	"os"
	"path/filepath"
	"testing"
)

// corpusRoot is the repository's corpus, relative to this package.
const corpusRoot = "../../evals/tasks"

// TestCorpusLints runs the linter over the committed corpus on every CI run: a broken task fails
// the build without the lab. MinCorpusTasks rises to 33 when the corpus is complete (Task 25).
func TestCorpusLints(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Fatalf("corpus directory missing: %v", err)
	}
	count, problems := Lint(filepath.Clean(corpusRoot))
	for _, p := range problems {
		t.Error(p)
	}
	if count < MinCorpusTasks {
		t.Errorf("corpus has %d tasks, the floor is %d", count, MinCorpusTasks)
	}
}
