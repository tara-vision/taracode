package evals

import (
	"os"
	"path/filepath"
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
