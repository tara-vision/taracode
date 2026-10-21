package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommonDirIsTheStoreBesideTheTasks(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	if got := CommonDir(tasksRoot); got != filepath.Join(evalsDir, "common") {
		t.Fatalf("%q", got)
	}
	if got := CommonDir(tasksRoot + string(filepath.Separator)); got != filepath.Join(evalsDir, "common") {
		t.Fatalf("a trailing separator names the same place: %q", got)
	}
	if got := CommonDir(filepath.Join(t.TempDir(), "tasks")); got != "" {
		t.Fatalf("no store: %q", got)
	}
}

func TestCorpusHashSkipsWhatTheMachineLeavesBeside(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	before, err := CorpusHash(tasksRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"tasks/.DS_Store", "tasks/crashloop-oomkilled/._task.yaml",
		"common/fixtures-recording-123/index.yaml", "tasks/crashloop-oomkilled/fixtures.replaced/index.yaml"} {
		p := filepath.Join(evalsDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	after, err := CorpusHash(tasksRoot)
	if err != nil || after != before {
		t.Fatalf("%v %q != %q", err, after, before)
	}
	if again, err := CorpusHash(tasksRoot + string(filepath.Separator)); err != nil || again != before {
		t.Fatalf("a trailing separator names the same corpus: %v %q", err, again)
	}
}

func TestCorpusHashWithoutACommonStore(t *testing.T) {
	tasksRoot := filepath.Join(t.TempDir(), "tasks")
	writeTask(t, tasksRoot, "crashloop-oomkilled", goodTask)
	if _, err := CorpusHash(tasksRoot); err != nil {
		t.Fatal(err)
	}
}

func TestCorpusHashReportsWhatItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	_, tasksRoot := sealedCorpus(t)
	if _, err := Seal(tasksRoot, "2"); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(tasksRoot, "crashloop-oomkilled", "task.yaml")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, err := CorpusHash(tasksRoot); err == nil || !strings.HasPrefix(err.Error(), "corpus hash: ") {
		t.Fatalf("an unreadable file: %v", err)
	}
	if _, err := Seal(tasksRoot, "3"); err == nil {
		t.Fatal("a seal over an unreadable file")
	}
	if problems := lintSeal(tasksRoot); len(problems) != 1 || !strings.HasPrefix(problems[0], "corpus hash: ") {
		t.Fatalf("lint: %v", problems)
	}
	_ = os.Chmod(locked, 0o644)
	dir := filepath.Join(tasksRoot, "crashloop-oomkilled")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := CorpusHash(tasksRoot); err == nil || !strings.HasPrefix(err.Error(), "corpus hash: ") {
		t.Fatalf("an unreadable directory: %v", err)
	}
}

func TestReadSealRefusesABadFile(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	seal := filepath.Join(evalsDir, sealFile)
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(seal, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("# the corpus seal\n\nversion: 2\nhash: " + strings.Repeat("a", 64) + "\n")
	if c, err := ReadSeal(tasksRoot); err != nil || c.Version != "2" {
		t.Fatalf("comments and blank lines: %+v %v", c, err)
	}
	for _, text := range []string{"version: 2\nsize: 3\n", "version: 2\nhash: xyz\n",
		"version: 2\nhash: " + strings.Repeat("a", 70) + "\n", "version: 2\nhash: " + strings.Repeat("a", 70000) + "\n"} {
		write(text)
		if _, err := ReadSeal(tasksRoot); err == nil {
			t.Fatalf("%q was read as a seal", text[:min(len(text), 30)])
		}
	}
	if err := os.Remove(seal); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(seal, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSeal(tasksRoot); err == nil {
		t.Fatal("a directory was read as a seal")
	}
	if problems := lintSeal(tasksRoot); len(problems) != 1 || !strings.Contains(problems[0], sealFile) {
		t.Fatalf("lint: %v", problems)
	}
	if _, err := Seal(tasksRoot, "5"); err == nil || !strings.Contains(err.Error(), sealFile) {
		t.Fatalf("a seal over a seal that cannot be read: %v", err)
	}
	if os.Geteuid() == 0 {
		return // root opens everything
	}
	if err := os.Remove(seal); err != nil {
		t.Fatal(err)
	}
	write("version: 2\nhash: " + strings.Repeat("a", 64) + "\n")
	if err := os.Chmod(seal, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(seal, 0o644) })
	if _, err := ReadSeal(tasksRoot); err == nil || !strings.Contains(err.Error(), sealFile) {
		t.Fatalf("a seal that cannot be opened: %v", err)
	}
}

func TestSealReportsAStoreItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes everywhere")
	}
	evalsDir, tasksRoot := sealedCorpus(t)
	if err := os.Chmod(evalsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(evalsDir, 0o755) })
	if _, err := Seal(tasksRoot, "2"); err == nil || !strings.HasPrefix(err.Error(), "seal: ") {
		t.Fatalf("%v", err)
	}
}

func TestTheFirstSealIsVersionTwo(t *testing.T) {
	_, tasksRoot := sealedCorpus(t)
	if _, err := Seal(tasksRoot, "1"); err == nil {
		t.Fatal("an unsealed corpus is already version 1, so its first seal takes a greater version")
	}
	if _, err := Seal(tasksRoot, "2"); err != nil {
		t.Fatal(err)
	}
}

func TestCorpusHashNamesTheTasksRootHoweverSpelled(t *testing.T) {
	evalsDir, tasksRoot := sealedCorpus(t)
	want, err := CorpusHash(tasksRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(tasksRoot)
	for _, spelling := range []string{".", "./", "../tasks", filepath.Join(evalsDir, "tasks") + "/"} {
		if got, err := CorpusHash(spelling); err != nil || got != want {
			t.Fatalf("%q: %v %q != %q", spelling, err, got, want)
		}
	}
}
