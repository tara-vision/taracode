package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func execTool(r *Registry, name string, args map[string]any, dir string) (string, error) {
	return r.Execute(context.Background(), name, args, dir)
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads and writes whatever the mode says")
	}
}

// bigLines is just over maxReadBytes: 61681 lines of 17 bytes.
var bigLines = strings.Repeat("0123456789abcdef\n", 61681)

func TestReadFileRefusals(t *testing.T) {
	r, dir := fileRegistry(t)
	writeTestFile(t, filepath.Join(dir, "big.log"), bigLines, 0o644)
	tests := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, "path is required"},
		{map[string]any{"path": "sub"}, "sub is a directory; use list_files"},
		{map[string]any{"path": "big.log"}, "big.log is 1048577 bytes; read it with start_line and end_line"},
	}
	for _, tt := range tests {
		if _, err := execTool(r, "read_file", tt.args, dir); err == nil || err.Error() != tt.want {
			t.Errorf("%v: %v, want %q", tt.args, err, tt.want)
		}
	}
	if out, err := execTool(r, "read_file", map[string]any{"path": "big.log", "end_line": 2.0}, dir); err != nil ||
		out != "0123456789abcdef\n0123456789abcdef\n" {
		t.Fatalf("a line range reads a large file: %q %v", out, err)
	}
	if out, err := execTool(r, "read_file", map[string]any{"path": "a.txt"}, dir); err != nil || out != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("no range reads it all: %q %v", out, err)
	}
}

func TestReadFileReportsAFileItCannotRead(t *testing.T) {
	skipAsRoot(t)
	r, dir := fileRegistry(t)
	writeTestFile(t, filepath.Join(dir, "locked.txt"), "x", 0o000)
	if _, err := execTool(r, "read_file", map[string]any{"path": "locked.txt"}, dir); err == nil ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestLineRange(t *testing.T) {
	const content = "a\nb\nc\n"
	tests := []struct {
		start, end int
		want       string
	}{
		{0, 0, content}, {2, 0, "b\nc\n"}, {0, 2, "a\nb\n"}, {3, 10, "c\n"}, {3, 2, ""},
	}
	for _, tt := range tests {
		if got := lineRange(content, tt.start, tt.end); got != tt.want {
			t.Errorf("lineRange(%d, %d) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestListFilesEdges(t *testing.T) {
	r, dir := fileRegistry(t)
	out, err := execTool(r, "list_files", map[string]any{}, dir)
	if err != nil || out != "a.txt\nsub/" {
		t.Fatalf("not recursive: %q %v", out, err)
	}
	out, err = execTool(r, "list_files", map[string]any{"recursive": true, "max": 1.0}, dir)
	if err != nil || out != "a.txt\n[1 entries shown; raise max or narrow the glob]" {
		t.Fatalf("limit: %q %v", out, err)
	}
	for _, args := range []map[string]any{{"path": "missing"}, {"glob": "*.tf"}} {
		if out, err := execTool(r, "list_files", args, dir); err != nil || out != "No entries" {
			t.Errorf("%v: %q %v", args, out, err)
		}
	}
}

func TestSearchFilesEdges(t *testing.T) {
	r, dir := fileRegistry(t)
	writeTestFile(t, filepath.Join(dir, "big.log"), "needle\n"+bigLines, 0o644)
	writeTestFile(t, filepath.Join(dir, "bin.dat"), "needle one\n\x00needle\nneedle two\n", 0o644)
	writeTestFile(t, filepath.Join(dir, "long.txt"), "needle"+strings.Repeat("x", 400)+"\n", 0o644)
	if _, err := execTool(r, "search_files", map[string]any{}, dir); err == nil || err.Error() != "pattern is required" {
		t.Fatalf("no pattern: %v", err)
	}
	out, err := execTool(r, "search_files", map[string]any{"pattern": "needle"}, dir)
	want := "bin.dat:1:needle one\nlong.txt:1:needle" + strings.Repeat("x", 294) + "..."
	if err != nil || out != want {
		t.Fatalf("large files skipped, binary files stop at the NUL, long lines cut:\n%q\n%v", out, err)
	}
	out, err = execTool(r, "search_files", map[string]any{"pattern": "needle", "max": 1.0}, dir)
	if err != nil || out != "bin.dat:1:needle one\n[1 matches shown; narrow the pattern or raise max]" {
		t.Fatalf("limit: %q %v", out, err)
	}
	if out, err := execTool(r, "search_files", map[string]any{"pattern": "one", "glob": "*.go"}, dir); err != nil ||
		out != "No matches found" {
		t.Fatalf("glob: %q %v", out, err)
	}
}

func TestSearchFilesSkipsWhatItCannotRead(t *testing.T) {
	skipAsRoot(t)
	r, dir := fileRegistry(t)
	writeTestFile(t, filepath.Join(dir, "locked.txt"), "TODO locked", 0o000)
	locked := filepath.Join(dir, "lockeddir")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(locked, "c.go"), "// TODO hidden\n", 0o644)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	out, err := execTool(r, "search_files", map[string]any{"pattern": "TODO"}, dir)
	if err != nil || out != "sub/b.go:1:package sub // TODO fix" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestWriteFileRefusals(t *testing.T) {
	r, dir := fileRegistry(t)
	tests := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"content": "x"}, "path is required"},
		{map[string]any{"path": "x.txt", "content": 5.0}, "content is required"},
		{map[string]any{"path": "a.txt/x.txt", "content": "x"}, "not a directory"},
		{map[string]any{"path": "sub", "content": "x"}, "is a directory"},
	}
	for _, tt := range tests {
		if _, err := execTool(r, "write_file", tt.args, dir); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestEditFileRefusals(t *testing.T) {
	r, dir := fileRegistry(t)
	tests := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"old": "one", "new": "1"}, "path is required"},
		{map[string]any{"path": "a.txt", "new": "1"}, "old is required"},
		{map[string]any{"path": "a.txt", "old": "", "new": "1"}, "old is required"},
		{map[string]any{"path": "gone.txt", "old": "one", "new": "1"}, "no such file or directory"},
	}
	for _, tt := range tests {
		if _, err := execTool(r, "edit_file", tt.args, dir); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestEditFileReportsAFileItCannotWrite(t *testing.T) {
	skipAsRoot(t)
	r, dir := fileRegistry(t)
	if err := os.Chmod(filepath.Join(dir, "a.txt"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := execTool(r, "edit_file", map[string]any{"path": "a.txt", "old": "one", "new": "1"}, dir); err == nil ||
		!strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("the file is unchanged: %q", data)
	}
}

func TestEditFileClassifiesAnEditAsAMutationOfItsFile(t *testing.T) {
	dir := t.TempDir()
	inv := editFileTool().Classify(map[string]any{"path": "conf/app.yaml", "old": "a", "new": "b"}, dir)
	if inv.Classification != policy.Mutate || inv.Reason != "edits a file" || len(inv.Targets.Paths) != 1 ||
		inv.Targets.Paths[0] != filepath.Join(dir, "conf", "app.yaml") {
		t.Fatalf("%+v", inv)
	}
}
