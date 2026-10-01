package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// filesTaskYAML is goodTask as a files task: no record block.
func filesTaskYAML(id string) string {
	return strings.NewReplacer("id: crashloop-oomkilled", "id: "+id, "provenance: recorded", "provenance: files").
		Replace(strings.SplitN(goodTask, "record:", 2)[0])
}

func TestLintReportsACorpusItCannotRead(t *testing.T) {
	count, problems := Lint(filepath.Join(t.TempDir(), "missing"))
	if count != 0 || len(problems) != 1 || !strings.Contains(problems[0], "missing") {
		t.Fatalf("count %d, problems %q", count, problems)
	}
}

func TestLintReportsTasksAndIndexesItCannotLoad(t *testing.T) {
	root := t.TempDir()
	writeTask(t, root, "broken-task", "id: broken-task\n")
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := "fixtures:\n  - {signature: kubectl get pods, file: ../escape.txt}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "index.yaml"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	count, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	if count != 1 || !strings.Contains(joined, "broken-task") ||
		!strings.Contains(joined, `crashloop-oomkilled: fixtures: `) || !strings.Contains(joined, "is not a plain file name") {
		t.Fatalf("count %d, problems:\n%s", count, joined)
	}
}

func TestLintFlagsAFilesTaskWithFixturesAndAMissingFixtureFile(t *testing.T) {
	root := t.TempDir()
	dir := writeTask(t, root, "files-task", filesTaskYAML("files-task"))
	if err := os.MkdirAll(filepath.Join(dir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	const sig = "kubectl get pod -n shop"
	if err := s.Save(sig, "ok", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "fixtures", fixtureFileName(sig))); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(root)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"files-task: a files task has fixtures; set provenance",
		"files-task: fixture file " + fixtureFileName(sig) + " missing"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems lack %q:\n%s", want, joined)
		}
	}
}
