package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestMainPrintsTheVersion runs the binary's entry point with --version: it prints the version and
// returns instead of exiting non-zero.
func TestMainPrintsTheVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	args := os.Args
	os.Args = []string{"taracode", "--version"}
	t.Cleanup(func() { os.Args = args })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	func() {
		defer func() { os.Stdout = stdout }() // also when main panics
		main()
	}()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "taracode version dev") {
		t.Fatalf("%q", out)
	}
}
