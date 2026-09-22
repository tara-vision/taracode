package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeBin puts an executable shell script named name first on PATH for the rest of the test. The
// script sees the arguments the tool passed; the default body echoes them back.
func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if script == "" {
		script = "#!/bin/sh\necho \"" + name + " $@\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil { //nolint:gosec // test helper
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
