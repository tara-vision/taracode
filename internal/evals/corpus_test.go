package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusRoot is the repository's corpus, relative to this package.
const corpusRoot = "../../evals/tasks"

// TestCorpusLints runs the linter over the committed corpus on every CI run: a broken task, or a corpus that changed
// under its seal, fails the build without the lab. MinCorpusTasks is the floor of the corpus.
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

// sealedCorpus is an evals directory with a tasks root holding one task and a common store with one fixture.
func sealedCorpus(t *testing.T) (evalsDir, tasksRoot string) {
	t.Helper()
	evalsDir = t.TempDir()
	tasksRoot = filepath.Join(evalsDir, "tasks")
	writeTask(t, tasksRoot, "crashloop-oomkilled", goodTask)
	common := filepath.Join(evalsDir, "common", "fixtures")
	if err := os.MkdirAll(common, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "index.yaml"), []byte("fixtures: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return evalsDir, tasksRoot
}

func TestCorpusHashIsStableAcrossFileOrder(t *testing.T) {
	_, tasksRoot := sealedCorpus(t)
	first, err := CorpusHash(tasksRoot)
	if err != nil || len(first) != 64 {
		t.Fatalf("hash %q %v", first, err)
	}
	again, _ := CorpusHash(tasksRoot)
	if again != first {
		t.Fatalf("the hash moved between two reads: %s %s", first, again)
	}
	other, otherRoot := sealedCorpus(t)
	_ = other
	if h, _ := CorpusHash(otherRoot); h != first {
		t.Fatalf("the same corpus in another directory hashes differently: %s %s", first, h)
	}
}

func TestCorpusHashChangesWithAFixture(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	before, _ := CorpusHash(tasksRoot)
	if err := os.WriteFile(filepath.Join(evalsDir, "common", "fixtures", "index.yaml"), []byte("fixtures:\n  - {signature: x, file: x.txt}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _ := CorpusHash(tasksRoot)
	if after == before {
		t.Fatal("a changed common fixture leaves the hash unchanged")
	}
	if err := os.WriteFile(filepath.Join(tasksRoot, "crashloop-oomkilled", "workdir-note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again, _ := CorpusHash(tasksRoot); again == after {
		t.Fatal("a new file under a task leaves the hash unchanged")
	}
}

func TestReadSealParsesVersionAndHash(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	if c, err := ReadSeal(tasksRoot); err != nil || c.Version != "1" || c.Hash != "" {
		t.Fatalf("a corpus without a seal is version 1: %+v %v", c, err)
	}
	if err := os.WriteFile(filepath.Join(evalsDir, "VERSION"), []byte("version: 2\nhash: "+strings.Repeat("ab", 32)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := ReadSeal(tasksRoot); err != nil || c.Version != "2" || c.Hash != strings.Repeat("ab", 32) {
		t.Fatalf("seal %+v %v", c, err)
	}
	if err := os.WriteFile(filepath.Join(evalsDir, "VERSION"), []byte("version: two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSeal(tasksRoot); err == nil || !strings.Contains(err.Error(), "VERSION") {
		t.Fatalf("a seal that is not a number and a hash: %v", err)
	}
}

func TestSealWritesTheVersionAndHashAndRefusesANonIncreasingVersion(t *testing.T) {
	_, tasksRoot := sealedCorpus(t)
	if _, err := Seal(tasksRoot, "2"); err != nil {
		t.Fatal(err)
	}
	c, err := ReadSeal(tasksRoot)
	hash, _ := CorpusHash(tasksRoot)
	if err != nil || c.Version != "2" || c.Hash != hash {
		t.Fatalf("seal %+v %v, hash %s", c, err, hash)
	}
	for _, v := range []string{"2", "1", "x", ""} {
		if _, err := Seal(tasksRoot, v); err == nil {
			t.Fatalf("version %q accepted after 2", v)
		}
	}
	if _, err := Seal(tasksRoot, "3"); err != nil {
		t.Fatal(err)
	}
}

func TestLintFailsWhenTheCorpusChangedUnderItsSeal(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	fixtures := filepath.Join(tasksRoot, "crashloop-oomkilled", "fixtures")
	if err := os.MkdirAll(fixtures, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "index.yaml"), []byte("fixtures:\n  - {signature: kubectl get pod -n shop, file: pods.txt}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "pods.txt"), []byte("NAME READY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(tasksRoot, "2"); err != nil {
		t.Fatal(err)
	}
	if _, problems := Lint(tasksRoot); len(problems) != 0 {
		t.Fatalf("a sealed corpus lints clean: %q", problems)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "pods.txt"), []byte("NAME READY STATUS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, problems := Lint(tasksRoot)
	if len(problems) != 1 || !strings.Contains(problems[0], "VERSION") || !strings.Contains(problems[0], "--seal") {
		t.Fatalf("a changed corpus under its seal: %q", problems)
	}
	if err := os.Remove(filepath.Join(evalsDir, "VERSION")); err != nil {
		t.Fatal(err)
	}
	if _, problems := Lint(tasksRoot); len(problems) != 0 {
		t.Fatalf("a corpus without a seal is not checked: %q", problems)
	}
}

func TestCheckCorpusVersionsRefusesTwoVersions(t *testing.T) {
	one := Results{Model: "a", Date: "2026-10-01"}
	two := Results{Model: "b", Date: "2026-11-01", Corpus: &Corpus{Version: "2", Hash: "x"}}
	if one.CorpusVersion() != "1" || two.CorpusVersion() != "2" {
		t.Fatalf("versions %q %q", one.CorpusVersion(), two.CorpusVersion())
	}
	if err := CheckCorpusVersions([]Results{two, two}); err != nil {
		t.Fatalf("one version: %v", err)
	}
	err := CheckCorpusVersions([]Results{one, two})
	if err == nil || !strings.Contains(err.Error(), ResultsName(one)) || !strings.Contains(err.Error(), ResultsName(two)) ||
		!strings.Contains(err.Error(), "version 1") || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("two versions: %v", err)
	}
}

func TestScoreboardCarriesTheCorpusVersion(t *testing.T) {
	all := sampleResults()
	for i := range all {
		all[i].Corpus = &Corpus{Version: "2", Hash: "x"}
	}
	sb := BuildScoreboard(all, 33, "3.4.0")
	if sb.CorpusVersion != "2" {
		t.Fatalf("corpus version %q", sb.CorpusVersion)
	}
	data, err := sb.JSON()
	if err != nil || !strings.Contains(string(data), `"corpus_version": "2"`) {
		t.Fatalf("json: %v\n%s", err, data)
	}
	if md := sb.Markdown(); !strings.Contains(strings.SplitN(md, "\n", 4)[2], "corpus v2") {
		t.Fatalf("markdown first lines:\n%s", strings.SplitN(md, "\n", 4)[2])
	}
	if sb := BuildScoreboard(sampleResults(), 33, "3.4.0"); sb.CorpusVersion != "1" {
		t.Fatalf("results without a corpus are version 1: %q", sb.CorpusVersion)
	}
}
