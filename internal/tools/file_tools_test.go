package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/policy"
)

func fileRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	r := NewRegistry(Options{})
	for _, tool := range FileTools() {
		r.Register(tool)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("package sub // TODO fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestReadFileWithLineRange(t *testing.T) {
	r, dir := fileRegistry(t)
	out, err := r.Execute(context.Background(), "read_file", map[string]any{"path": "a.txt", "start_line": 2.0, "end_line": 3.0}, dir)
	if err != nil || out != "two\nthree\n" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := r.Execute(context.Background(), "read_file", map[string]any{"path": "nope.txt"}, dir); err == nil {
		t.Error("missing file must error")
	}
	inv, _ := r.Classify("read_file", map[string]any{"path": "a.txt"}, dir)
	if inv.Classification != policy.Read {
		t.Errorf("%+v", inv)
	}
}

func TestListAndSearchFiles(t *testing.T) {
	r, dir := fileRegistry(t)
	out, err := r.Execute(context.Background(), "list_files", map[string]any{"path": ".", "recursive": true}, dir)
	if err != nil || !strings.Contains(out, "a.txt") || !strings.Contains(out, "sub/b.go") || strings.Contains(out, ".git") {
		t.Fatalf("%q %v", out, err)
	}
	out, err = r.Execute(context.Background(), "list_files", map[string]any{"glob": "*.go", "recursive": true}, dir)
	if err != nil || strings.Contains(out, "a.txt") || !strings.Contains(out, "b.go") {
		t.Fatalf("glob %q %v", out, err)
	}
	out, err = r.Execute(context.Background(), "search_files", map[string]any{"pattern": "TODO", "path": "."}, dir)
	if err != nil || !strings.Contains(out, "sub/b.go:1:") {
		t.Fatalf("%q %v", out, err)
	}
	out, _ = r.Execute(context.Background(), "search_files", map[string]any{"pattern": "nothing-here"}, dir)
	if !strings.Contains(out, "No matches") {
		t.Errorf("%q", out)
	}
	if _, err := r.Execute(context.Background(), "search_files", map[string]any{"pattern": "("}, dir); err == nil {
		t.Error("bad regexp must error")
	}
}

func TestWriteAndEditFileRecordHistoryAndClassifyMutate(t *testing.T) { //nolint:gocyclo // one sequential scenario exercising write, edit, history and preview together, as specified
	r, dir := fileRegistry(t)
	hm, err := history.NewManager(filepath.Join(dir, ".taracode"), "s1")
	if err != nil {
		t.Fatal(err)
	}
	r.SetHistory(hm)
	if _, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "new/c.txt", "content": "hello\n"}, dir); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "new", "c.txt")); string(data) != "hello\n" {
		t.Fatalf("written %q", data)
	}
	out, err := r.Execute(context.Background(), "edit_file", map[string]any{"path": "a.txt", "old": "two", "new": "2"}, dir)
	if err != nil || !strings.Contains(out, "Edited") {
		t.Fatalf("%q %v", out, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "one\n2\nthree\nfour\n" {
		t.Fatalf("edited %q", data)
	}
	if _, err := r.Execute(context.Background(), "edit_file", map[string]any{"path": "a.txt", "old": "missing", "new": "x"}, dir); err == nil {
		t.Error("missing old text must error")
	}
	if err := os.WriteFile(filepath.Join(dir, "dup.txt"), []byte("x x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), "edit_file", map[string]any{"path": "dup.txt", "old": "x", "new": "y"}, dir); err == nil || !strings.Contains(err.Error(), "2 matches") {
		t.Errorf("ambiguous edit: %v", err)
	}
	ops := hm.GetAllHistory()
	if len(ops) != 2 || ops[0].Tool != "write_file" || ops[1].Tool != "edit_file" || ops[1].BackupPath == "" {
		t.Fatalf("history %+v", ops)
	}
	inv, _ := r.Classify("write_file", map[string]any{"path": "x.txt"}, dir)
	if inv.Classification != policy.Mutate || len(inv.Targets.Paths) != 1 || inv.Targets.Paths[0] != filepath.Join(dir, "x.txt") {
		t.Errorf("%+v", inv)
	}
	preview, _ := r.Classify("edit_file", map[string]any{"path": "a.txt", "old": "a", "new": "b", "preview": true}, dir)
	if preview.Classification != policy.Read {
		t.Errorf("preview must be a read: %+v", preview)
	}
	out, err = r.Execute(context.Background(), "edit_file", map[string]any{"path": "a.txt", "old": "three", "new": "3", "preview": true}, dir)
	if err != nil || !strings.Contains(out, "-three") || !strings.Contains(out, "+3") {
		t.Fatalf("preview diff %q %v", out, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); strings.Contains(string(data), "\n3\n") {
		t.Fatal("preview must not write")
	}
}

func TestPathsStayInsideTheWorkingDirectoryUnlessAbsolute(t *testing.T) {
	_, dir := fileRegistry(t)
	if got := resolvePath("sub/../a.txt", dir); got != filepath.Join(dir, "a.txt") {
		t.Errorf("%q", got)
	}
	if got := resolvePath("/etc/hosts", dir); got != "/etc/hosts" {
		t.Errorf("%q", got)
	}
}
