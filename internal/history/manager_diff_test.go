package history

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateUnifiedDiff(t *testing.T) {
	tests := []struct {
		name, oldContent, newContent, want string
	}{
		{"identical", "a\nb\n", "a\nb\n", "--- old\n+++ new\n"},
		{"changed and appended", "a\nb\nc\n", "a\nB\nc\nd\n", "--- old\n+++ new\n@@ -2,2 +2,3 @@\n-b\n+B\n c\n+d\n"},
		{"first line", "a\nb\n", "z\nb\n", "--- old\n+++ new\n@@ -1,2 +1,2 @@\n-a\n+z\n b\n"},
	}
	for _, tt := range tests {
		if got := generateUnifiedDiff("old", "new", tt.oldContent, tt.newContent); got != tt.want {
			t.Errorf("%s: diff\n%q\nwant\n%q", tt.name, got, tt.want)
		}
	}
	// A created or emptied file: only the lines are checked, not the empty side's start line in the
	// hunk header (it is 1 here where diff(1) writes 0; git apply takes either).
	if got := generateUnifiedDiff("/dev/null", "new", "", "x\ny\n"); !strings.HasPrefix(got, "--- /dev/null\n+++ new\n@@ ") ||
		!strings.HasSuffix(got, " @@\n+x\n+y\n") {
		t.Errorf("created: %q", got)
	}
	if got := generateUnifiedDiff("old", "/dev/null", "x\n", ""); !strings.HasPrefix(got, "--- old\n+++ /dev/null\n@@ ") ||
		!strings.HasSuffix(got, " @@\n-x\n") {
		t.Errorf("emptied: %q", got)
	}
}

func TestComputeLCS(t *testing.T) {
	tests := []struct {
		a, b []string
		want string
	}{
		{nil, []string{"a"}, ""},
		{[]string{"a"}, nil, ""},
		{[]string{"a", "b", "c", "d"}, []string{"b", "x", "d"}, "b,d"},
		{[]string{"q", "a", "r", "b"}, []string{"a", "s", "b", "t"}, "a,b"},
	}
	for _, tt := range tests {
		if got := strings.Join(computeLCS(tt.a, tt.b), ","); got != tt.want {
			t.Errorf("computeLCS(%v, %v) = %s, want %s", tt.a, tt.b, got, tt.want)
		}
	}
}

// diffFixture records every kind of change on real files under dir, plus the operations
// GenerateDiff must leave out: a read, a failed write, an undone write and a write whose file is gone.
func diffFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	m, _ := newTestManager(t)
	dir := t.TempDir()
	modified, backup, created := filepath.Join(dir, "app.txt"), filepath.Join(dir, "app.txt.bak"), filepath.Join(dir, "new.txt")
	writeTestFile(t, modified, "a\nB\n")
	writeTestFile(t, backup, "a\nb\n")
	writeTestFile(t, created, "fresh\n")
	ops := []Operation{
		{Tool: "edit_file", Type: OpTypeEdit, Target: modified, BackupPath: backup, Success: true},
		{Tool: "write_file", Type: OpTypeWrite, Target: created, Success: true},
		{Tool: "delete_file", Type: OpTypeDelete, Target: filepath.Join(dir, "gone.txt"), DeletedContent: "bye\n", Success: true},
		{Tool: "delete_file", Type: OpTypeDelete, Target: filepath.Join(dir, "big.bin"), Success: true},
		{Tool: "move_file", Type: OpTypeMove, Target: filepath.Join(dir, "moved.txt"), OriginalPath: filepath.Join(dir, "orig.txt"),
			Success: true},
		{Tool: "copy_file", Type: OpTypeCopy, Target: filepath.Join(dir, "copy.txt"), OriginalPath: filepath.Join(dir, "src.txt"),
			Success: true},
		{Tool: "create_directory", Type: OpTypeCreate, Target: filepath.Join(dir, "conf"), Success: true},
		{Tool: "read_file", Type: OpTypeRead, Target: filepath.Join(dir, "read.txt"), Success: true},
		{Tool: "write_file", Type: OpTypeWrite, Target: filepath.Join(dir, "failed.txt"), Success: false},
		{Tool: "write_file", Type: OpTypeWrite, Target: filepath.Join(dir, "undone.txt"), Success: true, Undone: true},
		{Tool: "write_file", Type: OpTypeWrite, Target: filepath.Join(dir, "vanished.txt"), Success: true},
		{Tool: "edit_file", Type: OpTypeEdit, Target: filepath.Join(dir, "lost-backup.txt"), BackupPath: filepath.Join(dir, "nope.bak"),
			Success: true},
	}
	for _, op := range ops {
		if err := m.Record(op); err != nil {
			t.Fatal(err)
		}
	}
	return m, dir
}

func TestGenerateDiffDescribesEachFileOnce(t *testing.T) {
	m, dir := diffFixture(t)
	diffs, err := m.GenerateDiff()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]FileDiff{}
	var order []string
	for _, d := range diffs {
		got[filepath.Base(d.Path)] = d
		order = append(order, filepath.Base(d.Path))
	}
	if strings.Join(order, ",") != "app.txt,big.bin,conf,copy.txt,gone.txt,moved.txt,new.txt" {
		t.Fatalf("diffs for %v: sorted by path, without reads, failures, undone writes or unreadable files", order)
	}
	app := filepath.Join(dir, "app.txt")
	want := map[string]FileDiff{
		"app.txt": {Path: app, Operation: "modified", Diff: "--- a/" + app + "\n+++ b/" + app + "\n@@ -2,1 +2,1 @@\n-b\n+B\n"},
		"new.txt": {Path: filepath.Join(dir, "new.txt"), Operation: "created",
			Diff: generateUnifiedDiff("/dev/null", filepath.Join(dir, "new.txt"), "", "fresh\n")},
		"gone.txt": {Path: filepath.Join(dir, "gone.txt"), Operation: "deleted",
			Diff: generateUnifiedDiff("a/"+filepath.Join(dir, "gone.txt"), "/dev/null", "bye\n", "")},
		"big.bin": {Path: filepath.Join(dir, "big.bin"), Operation: "deleted"},
		"moved.txt": {Path: filepath.Join(dir, "moved.txt"), Operation: "moved", OriginalPath: filepath.Join(dir, "orig.txt"),
			Diff: "rename from " + filepath.Join(dir, "orig.txt") + "\nrename to " + filepath.Join(dir, "moved.txt") + "\n"},
		"copy.txt": {Path: filepath.Join(dir, "copy.txt"), Operation: "copied", OriginalPath: filepath.Join(dir, "src.txt"),
			Diff: "copy from " + filepath.Join(dir, "src.txt") + "\ncopy to " + filepath.Join(dir, "copy.txt") + "\n"},
		"conf": {Path: filepath.Join(dir, "conf"), Operation: "created", Diff: "new directory " + filepath.Join(dir, "conf") + "\n"},
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s:\n got %+v\nwant %+v", name, got[name], w)
		}
	}
}

func TestGenerateDiffSkipsAModifiedFileThatIsGone(t *testing.T) {
	m, _ := newTestManager(t)
	dir := t.TempDir()
	backup := filepath.Join(dir, "app.txt.bak")
	writeTestFile(t, backup, "old\n")
	if err := m.Record(Operation{Tool: "edit_file", Type: OpTypeEdit, Target: filepath.Join(dir, "app.txt"), BackupPath: backup,
		Success: true}); err != nil {
		t.Fatal(err)
	}
	if diffs, err := m.GenerateDiff(); err != nil || len(diffs) != 0 {
		t.Fatalf("diffs %+v err=%v", diffs, err)
	}
}

func TestExportDiffWritesAHeaderPerFile(t *testing.T) {
	m, dir := diffFixture(t)
	out, err := m.ExportDiff()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# modified: " + filepath.Join(dir, "app.txt") + "\n--- a/",
		"# moved: " + filepath.Join(dir, "moved.txt") + "\n# (from " + filepath.Join(dir, "orig.txt") + ")\nrename from",
		"# created: " + filepath.Join(dir, "conf") + "\nnew directory",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export lacks %q:\n%s", want, out)
		}
	}
	empty, _ := newTestManager(t)
	if out, err := empty.ExportDiff(); err != nil || out != "" {
		t.Fatalf("no changes export nothing: %q %v", out, err)
	}
}

func TestGetModifiedFiles(t *testing.T) {
	m, dir := diffFixture(t)
	if err := m.RecordOperation("write_file", nil, filepath.Join(dir, "new.txt"), true, "ok", ""); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range m.GetModifiedFiles() {
		names = append(names, filepath.Base(f))
	}
	if got := strings.Join(names, ","); got !=
		"app.txt,big.bin,conf,copy.txt,gone.txt,lost-backup.txt,moved.txt,new.txt,vanished.txt" {
		t.Fatalf("GetModifiedFiles() = %s: unique, sorted, successful and not undone", got)
	}
}
