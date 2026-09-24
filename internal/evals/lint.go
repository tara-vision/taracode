package evals

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
		problems = append(problems, lintWorkdir(t)...)
		problems = append(problems, lintLeftoverRecording(t)...)
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

// lintFixtureContents flags a missing fixture file, a raw secret in any regular file under
// fixtures/ (indexed, orphaned or a dotfile, index.yaml included: ruling P3-R38 - the dotfile
// exemption below is for the "not in the index" message only, not for the secret scan), a file two
// signatures share, and an orphan file.
func lintFixtureContents(t Task, store *Store) []string {
	var problems []string
	referenced, seenFiles := map[string]bool{}, map[string]bool{}
	for _, f := range store.Fixtures() {
		if seenFiles[f.File] {
			problems = append(problems, t.ID+": fixture file "+f.File+" is shared by multiple signatures")
		}
		seenFiles[f.File], referenced[f.File] = true, true
		if _, err := os.Stat(filepath.Join(store.Dir(), f.File)); err != nil {
			problems = append(problems, t.ID+": fixture file "+f.File+" missing")
		}
	}
	_ = filepath.WalkDir(store.Dir(), func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(store.Dir(), p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		label := "fixture " + rel
		if rel == "index.yaml" {
			label = "fixtures index.yaml"
		}
		data, err := os.ReadFile(p) //nolint:gosec // p walks store.Dir(), a trusted corpus path
		if err != nil {
			// A file that exists (WalkDir found it) but cannot be read (mode 000, a dangling
			// symlink, ...) must not silently skip both the secret scan and the orphan check below:
			// that would be a lint blind spot, not a clean fixture.
			problems = append(problems, t.ID+": "+label+" cannot be read: "+err.Error())
			return nil
		}
		if fixtureCarriesASecret(data) {
			problems = append(problems, t.ID+": "+label+" carries a raw secret pattern")
		}
		if rel != "index.yaml" && !referenced[rel] && !isDotPath(rel) {
			problems = append(problems, t.ID+": fixture file "+rel+" is not in the index")
		}
		return nil
	})
	return problems
}

// isDotPath reports whether any component of a slash-separated relative path starts with a dot, the
// convention a hidden file or directory (.DS_Store, .git, ...) uses to say "not real content".
func isDotPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// rawSecret is a narrower, boundary-free lint-only check kept alongside redact.ContainsSecret
// because each catches shapes the other misses: a PRIVATE KEY header with a body but no END line (the
// redactor's pattern needs both), and an AWS key or a GitHub token glued to another word character
// (the redactor's patterns are \b-anchored so a model or a scenario script that concatenates one
// mid-word defeats them). A false positive here is the cheap direction (ruling P3-R38).
var rawSecret = regexp.MustCompile(`AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----\n[A-Za-z0-9+/=]{20,}|` +
	`ghp_[A-Za-z0-9]{36}|xox[abp]-[0-9A-Za-z-]{10,}`)

// fixtureCarriesASecret flags data as a raw secret if either rawSecret or redact.ContainsSecret (the
// same, thorough patterns the recorder redacts with) matches it.
func fixtureCarriesASecret(data []byte) bool {
	if rawSecret.Match(data) {
		return true
	}
	_, found := redact.ContainsSecret(string(data))
	return found
}

// lintWorkdir flags any symlink under a task's workdir/: it is copied into a fresh run directory
// verbatim (record.go's copyDir), and initial task content is meant to be plain, reproducible files,
// not a link that behaves differently across machines and platforms (ruling P3-R38). It walks with
// Lstat (via fs.DirEntry, which never follows a symlink to decide this) so a symlinked directory is
// reported itself rather than walked into.
func lintWorkdir(t Task) []string {
	root := filepath.Join(t.Dir, "workdir")
	var problems []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		problems = append(problems, t.ID+": workdir/"+filepath.ToSlash(rel)+" is a symlink")
		return nil
	})
	return problems
}

// lintLeftoverRecording flags a fixtures-recording-* or fixtures.replaced directory left directly
// under a task directory (ruling P3-R41 item 5): record.go's RecordTask only leaves one behind when a
// run crashed before cleaning up, or when the final swap into fixtures/ failed, so its presence means
// the task's fixtures may be stale or incomplete. The fix is to run the recorder again, not to edit
// around it, so the message says so.
func lintLeftoverRecording(t Task) []string {
	entries, err := os.ReadDir(t.Dir)
	if err != nil {
		return nil
	}
	var problems []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "fixtures.replaced" || strings.HasPrefix(name, "fixtures-recording-") {
			problems = append(problems, t.ID+": leftover "+name+" from an interrupted recording; run make record again")
		}
	}
	return problems
}
