package evals

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tara-vision/taracode/internal/tools/redact"
)

// Lint loads every task under root and checks what LoadTask cannot see alone: unique ids, the
// policy file of an operate task, the workdir of a files task, and the fixtures. It returns the
// task count and the problems, one per line.
func Lint(root string) (int, []string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, []string{err.Error()}
	}
	var problems []string
	seen := map[string]bool{}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		t, err := LoadTask(dir)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		count++
		if seen[t.ID] {
			problems = append(problems, t.ID+": duplicate id")
		}
		seen[t.ID] = true
		problems = append(problems, lintFixtures(t)...)
		if t.Policy != "" {
			if _, err := os.Stat(filepath.Join(dir, t.Policy)); err != nil {
				problems = append(problems, t.ID+": policy file "+t.Policy+" missing")
			}
		}
		if t.Provenance == ProvenanceFiles {
			if _, err := os.Stat(filepath.Join(dir, "workdir")); err != nil {
				problems = append(problems, t.ID+": a files task needs a workdir")
			}
		}
	}
	return count, problems
}

// lintFixtures checks whether a task needs a fixtures index at all: recorded and authored tasks
// need a non-empty one, a files task should have none. lintFixtureContents then scans whatever is
// actually on disk either way, since a files task's fixtures directory, if one exists at all, needs
// the same content checks.
func lintFixtures(t Task) []string {
	store, err := LoadFixtures(t.Dir)
	if err != nil {
		return []string{t.ID + ": fixtures: " + err.Error()}
	}
	var problems []string
	switch {
	case t.Provenance == ProvenanceFiles && store.Len() > 0:
		problems = append(problems, t.ID+": a files task has fixtures; set provenance")
	case t.Provenance != ProvenanceFiles && store.Len() == 0:
		problems = append(problems, t.ID+": fixtures index is empty (run make record, or author them)")
	}
	return append(problems, lintFixtureContents(t, store)...)
}

// lintFixtureContents flags a missing fixture file, a raw secret in a fixture file or in
// index.yaml itself, a file two signatures share, and an orphan file (a dotfile such as .DS_Store
// excepted). It uses redact.ContainsSecret, the same patterns the recorder redacts with, instead of
// a narrower lint-only list, so text the recorder already redacted reads clean (ruling P3-R28).
func lintFixtureContents(t Task, store *Store) []string {
	var problems []string
	referenced, seenFiles := map[string]bool{}, map[string]bool{}
	for _, f := range store.Fixtures() {
		if seenFiles[f.File] {
			problems = append(problems, t.ID+": fixture file "+f.File+" is shared by multiple signatures")
		}
		seenFiles[f.File], referenced[f.File] = true, true
		data, err := os.ReadFile(filepath.Join(store.Dir(), f.File))
		if err != nil {
			problems = append(problems, t.ID+": fixture file "+f.File+" missing")
			continue
		}
		if _, found := redact.ContainsSecret(string(data)); found {
			problems = append(problems, t.ID+": fixture "+f.File+" carries a raw secret pattern")
		}
	}
	if data, err := os.ReadFile(filepath.Join(store.Dir(), "index.yaml")); err == nil {
		if _, found := redact.ContainsSecret(string(data)); found {
			problems = append(problems, t.ID+": fixtures index.yaml carries a raw secret pattern")
		}
	}
	files, _ := os.ReadDir(store.Dir())
	for _, f := range files {
		if f.Name() == "index.yaml" || referenced[f.Name()] || strings.HasPrefix(f.Name(), ".") {
			continue
		}
		problems = append(problems, t.ID+": fixture file "+f.Name()+" is not in the index")
	}
	return problems
}
