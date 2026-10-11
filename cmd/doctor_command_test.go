package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/provider"
)

// TestDoctorCommandPrintsTheReport runs `taracode doctor` against a reachable fake: the report,
// with the policy line for the current directory, and no error.
func TestDoctorCommandPrintsTheReport(t *testing.T) {
	resetConfig(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	srv := fakeEngine(t)
	viper.Set("host", srv.URL)
	viper.Set("model", "gemma4:12b")
	var err error
	out := captureStdoutForTest(t, func() { _, err = executeWith(t, "doctor") })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"taraengine 0.2.0 (runtime b11514, linux/amd64/cuda12)", "gemma4:12b", "Policy    ok (built-in)"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}
}

// TestDoctorCommandAsksTheDefaultHostWhenNoneIsSet: a missing host no longer stops doctor; it checks the local
// engine's address like every command. The command itself is not run: on a machine with no engine it exits 1.
func TestDoctorCommandAsksTheDefaultHostWhenNoneIsSet(t *testing.T) {
	resetConfig(t)
	isolateHome(t)
	if host, _, _, _ := resolveDoctorTarget(); host != provider.DefaultHost {
		t.Fatalf("host = %q, want %s", host, provider.DefaultHost)
	}
}

func TestRunDoctorNeedsAHost(t *testing.T) {
	if _, err := runDoctor(context.Background(), "", "", "", ""); err == nil ||
		!strings.Contains(err.Error(), "doctor: create provider: host is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestPolicyNoteReportsABrokenPolicy(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".taracode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".taracode", "policy.yaml"), "version: 1\nprotected:\n  pths: []\n")
	if note := policyNote(dir); !strings.HasPrefix(note, "error: ") || !strings.Contains(note, "pths") {
		t.Fatalf("policyNote() = %q", note)
	}
}

func TestDoctorSlashCommandUsesTheProjectRoot(t *testing.T) {
	r, _ := projectREPL(t)
	out := captureStdoutForTest(t, func() { r.dispatch("/doctor") })
	if !strings.Contains(out, "gemma4:12b") || !strings.Contains(out, "Policy    ok (built-in)") {
		t.Fatalf("%q", out)
	}
}
