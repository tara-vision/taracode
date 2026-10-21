package evals

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCommonCallsRefusesABadFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadCommonCalls(filepath.Join(dir, "calls.yaml")); err == nil {
		t.Fatal("a missing file")
	}
	path := writeCommonCalls(t, dir, "calls: [\n")
	if _, err := LoadCommonCalls(path); err == nil || !strings.Contains(err.Error(), "calls.yaml") {
		t.Fatalf("bad yaml: %v", err)
	}
	path = writeCommonCalls(t, dir, "calls:\n  - {tool: shell, args: {command: \"true\"}, dry_run: true}\n")
	if _, err := LoadCommonCalls(path); err == nil || !strings.Contains(err.Error(), "dry run") {
		t.Fatalf("a dry run: %v", err)
	}
}

func TestRecordCommonRefusesWhatItCannotSetUp(t *testing.T) {
	dir := t.TempDir()
	goneTemp := filepath.Join(t.TempDir(), "gone")
	err := RecordCommon(context.Background(), dir, nil, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "common: ") || !strings.Contains(err.Error(), "calls.yaml") {
		t.Fatalf("no calls.yaml: %v", err)
	}
	writeCommonCalls(t, dir, "calls:\n  - {tool: shell, args: {command: \"true\"}}\n")
	if os.Geteuid() != 0 {
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		err := RecordCommon(context.Background(), dir, nil, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "fixtures-recording") {
			t.Fatalf("a read-only store: %v", err)
		}
		_ = os.Chmod(dir, 0o755)
	}
	t.Setenv("TMPDIR", goneTemp)
	err = RecordCommon(context.Background(), dir, nil, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "common: ") || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("no temp directory: %v", err)
	}
}
