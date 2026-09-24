package evals

import (
	"os"
	"path/filepath"
	"regexp"
)

// Lint loads every task under root and checks what LoadTask cannot see alone: unique ids, the
// policy file of an operate task, the workdir of a files task, and the fixtures (Task 10 adds
// lintFixtures). It returns the task count and the problems, one per line.
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

// rawSecret patterns must never appear in a fixture: the recorder stores redacted text.
var rawSecret = regexp.MustCompile(`AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----\n[A-Za-z0-9+/=]{20,}|` +
	`ghp_[A-Za-z0-9]{36}|xox[abp]-[0-9A-Za-z-]{10,}`)

// lintFixtures checks a task's fixtures: recorded and authored tasks need a non-empty index whose
// files all exist, with no orphan file and no raw secret; a files task has none.
func lintFixtures(t Task) []string {
	store, err := LoadFixtures(t.Dir)
	if err != nil {
		return []string{t.ID + ": fixtures: " + err.Error()}
	}
	if t.Provenance == ProvenanceFiles {
		if store.Len() > 0 {
			return []string{t.ID + ": a files task has fixtures; set provenance"}
		}
		return nil
	}
	if store.Len() == 0 {
		return []string{t.ID + ": fixtures index is empty (run make record, or author them)"}
	}
	var problems []string
	referenced := map[string]bool{"index.yaml": true}
	for _, f := range store.Fixtures() {
		referenced[f.File] = true
		data, err := os.ReadFile(filepath.Join(store.Dir(), f.File))
		if err != nil {
			problems = append(problems, t.ID+": fixture file "+f.File+" missing")
			continue
		}
		if rawSecret.Match(data) {
			problems = append(problems, t.ID+": fixture "+f.File+" carries a raw secret pattern")
		}
	}
	files, _ := os.ReadDir(store.Dir())
	for _, f := range files {
		if !referenced[f.Name()] {
			problems = append(problems, t.ID+": fixture file "+f.Name()+" is not in the index")
		}
	}
	return problems
}
