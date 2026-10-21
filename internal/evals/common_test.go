package evals

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCommonCalls(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, "calls.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCommonCallsReadsShellCallsOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeCommonCalls(t, dir, "calls:\n  - {tool: shell, args: {command: \"which terraform\"}}\n  - {tool: shell, args: {command: \"terraform version\"}}\n")
	calls, err := LoadCommonCalls(path)
	if err != nil || len(calls) != 2 || calls[1].Tool != "shell" || calls[1].Args["command"] != "terraform version" {
		t.Fatalf("calls %+v, %v", calls, err)
	}
	path = writeCommonCalls(t, dir, "calls:\n  - {tool: kubectl, args: {verb: version}}\n")
	if _, err := LoadCommonCalls(path); err == nil || !strings.Contains(err.Error(), "shell") || !strings.Contains(err.Error(), "calls.yaml") {
		t.Fatalf("a kubectl call in the common calls: %v", err)
	}
	path = writeCommonCalls(t, dir, "calls:\n  - {tool: shell, args: {command: \"echo x\"}, env: {KUBECONFIG: /none}}\n")
	if _, err := LoadCommonCalls(path); err == nil {
		t.Fatal("a common call carries no env")
	}
}

func TestRecordCommonWritesTheStore(t *testing.T) {
	dir := t.TempDir()
	writeCommonCalls(t, dir, "calls:\n  - {tool: shell, args: {command: \"echo hello\"}}\n  - {tool: shell, args: {command: \"true\"}}\n")
	if err := RecordCommon(context.Background(), dir, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	store, err := LoadFixtures(dir)
	if err != nil || store.Len() != 2 {
		t.Fatalf("store %d, %v", store.Len(), err)
	}
	if out, isErr, ok := store.Lookup("shell echo hello"); !ok || isErr || out != "hello" { // as the shell tool shows it
		t.Fatalf("echo: %q %v %v", out, isErr, ok)
	}
	writeCommonCalls(t, dir, "calls:\n  - {tool: shell, args: {command: \"true\"}}\n")
	if err := RecordCommon(context.Background(), dir, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if store, _ := LoadFixtures(dir); store.Len() != 1 {
		t.Fatalf("a second recording replaces the store: %d fixtures", store.Len())
	}
}
