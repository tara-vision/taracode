package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionsThatDoNotParse(t *testing.T) {
	if versionBefore("0.5.0", "not.a.version") {
		t.Fatal("an unparsable requirement is never newer")
	}
	if _, ok := parseVersion("1.2.3.4"); ok {
		t.Fatal("four components")
	}
}

// fakeSysctl puts a sysctl script first on PATH.
func fakeSysctl(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("HostRAMGB asks sysctl on macOS only")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sysctl"), []byte(script), 0o755); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestHostRAMGBReportsSysctlFailures(t *testing.T) {
	fakeSysctl(t, "#!/bin/sh\nexit 1\n")
	if _, err := HostRAMGB(); err == nil || !strings.HasPrefix(err.Error(), "models: sysctl hw.memsize: ") {
		t.Fatalf("a failing sysctl: %v", err)
	}
	fakeSysctl(t, "#!/bin/sh\necho lots\n")
	if _, err := HostRAMGB(); err == nil || !strings.HasPrefix(err.Error(), "models: parse hw.memsize: ") {
		t.Fatalf("an unreadable answer: %v", err)
	}
	fakeSysctl(t, "#!/bin/sh\necho 68719476736\n")
	if gb, err := HostRAMGB(); err != nil || gb != 64 {
		t.Fatalf("64 GiB: %d %v", gb, err)
	}
}
