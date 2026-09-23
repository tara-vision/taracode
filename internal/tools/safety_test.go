package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

// TestShellTargetsReachThePolicy: a shell write names its files in Targets.Paths, resolved against
// the working directory, ~ and $HOME expanded and globs matched on disk, so the protected paths,
// the policy files included, apply to it (final review I1). The probes are the reviewer's.
func TestShellTargetsReachThePolicy(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(dir, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "infra", "terraform.tfstate"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	shell := ShellTool(nil)
	targets := func(command string) []string {
		return shell.Classify(map[string]any{"command": command}, dir).Targets.Paths
	}
	for command, want := range map[string]string{
		"echo x > .taracode/policy.yaml":              filepath.Join(dir, ".taracode", "policy.yaml"),
		"rm ~/.taracode/policy.yaml":                  filepath.Join(home, ".taracode", "policy.yaml"),
		"sed -i 's/a/b/' $HOME/.taracode/policy.yaml": filepath.Join(home, ".taracode", "policy.yaml"),
		"rm -f infra/*":                               filepath.Join(dir, "infra", "terraform.tfstate"),
		"cd infra && echo '{}' > terraform.tfstate":   filepath.Join(dir, "infra", "terraform.tfstate"),
	} {
		if got := targets(command); !containsString(got, want) {
			t.Errorf("%q: targets %v should include %s", command, got, want)
		}
	}
	pol, _, err := policy.Load(dir, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"echo x > .taracode/policy.yaml", "cp /tmp/p.yaml ~/.taracode/policy.yaml",
		"echo '{}' > infra/terraform.tfstate", "rm -f infra/*", "cd .taracode && echo x > policy.yaml"} {
		inv := shell.Classify(map[string]any{"command": command}, dir)
		inv.WorkingDir = dir
		if v := pol.Evaluate(policy.ModeOperate, inv); v.Allow || v.Rule != "protected.paths" {
			t.Errorf("%q: %+v", command, v)
		}
	}
	inv := shell.Classify(map[string]any{"command": "echo x > notes.txt"}, dir)
	inv.WorkingDir = dir
	if v := pol.Evaluate(policy.ModeOperate, inv); !v.Allow {
		t.Errorf("a write to an unprotected file goes on to the permission: %+v", v)
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestRegistryExposedMatchesTheDefinitions: Exposed answers for one tool what Definitions offers,
// so the loop can refuse a call to a tool the model was not offered (final review I3).
func TestRegistryExposedMatchesTheDefinitions(t *testing.T) {
	r := NewRegistry(Options{Offline: true})
	r.Register(newTestTool("reader", true, policy.Read))
	r.Register(newTestTool("writer", false, policy.Mutate))
	ext := newTestTool("ext", true, policy.Read)
	ext.External = true
	r.Register(ext)
	cases := []struct {
		name string
		mode policy.Mode
		want bool
	}{
		{"reader", policy.ModeInvestigate, true}, {"reader", policy.ModeOperate, true},
		{"writer", policy.ModeInvestigate, false}, {"writer", policy.ModeOperate, true},
		{"ext", policy.ModeInvestigate, false}, {"ext", policy.ModeOperate, false},
		{"missing", policy.ModeOperate, false},
	}
	for _, c := range cases {
		if got := r.Exposed(c.name, c.mode); got != c.want {
			t.Errorf("Exposed(%s, %s) = %v", c.name, c.mode, got)
		}
	}
}

// TestLiveShellStreamIsRedactedLineByLine: the registry puts the configured redactor in front of the
// shell tool's live stream, so a secret a command prints never reaches the screen raw, a line
// without a newline included (final review I4); without a redactor the stream stays raw.
func TestLiveShellStreamIsRedactedLineByLine(t *testing.T) {
	red, _ := redact.New(redact.Options{})
	var screen bytes.Buffer
	r := NewBuiltinRegistry(Options{Redactor: red}, Config{Stream: &screen})
	command := `printf 'key=AKIAIOSFODNN7EXAMPLE\nlast AKIAIOSFODNN7EXAMPLE'`
	out, err := r.Execute(context.Background(), "shell", map[string]any{"command": command}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(screen.String(), "AKIAIOSFODNN7EXAMPLE") ||
		screen.String() != "key=[redacted:aws-access-key]\nlast [redacted:aws-access-key]" {
		t.Fatalf("the live stream must be redacted line by line, the tail at the end: %q", screen.String())
	}
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") || r.Redactions() != 2 {
		t.Fatalf("the result is redacted and each secret counted once: %q %d", out, r.Redactions())
	}
	var raw bytes.Buffer
	plain := NewBuiltinRegistry(Options{}, Config{Stream: &raw})
	if _, err := plain.Execute(context.Background(), "shell", map[string]any{"command": command}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw.String(), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("without a redactor the stream is the raw output: %q", raw.String())
	}
}

// TestWriteIsRefusedWhenTheBackupFails: write_file and edit_file never overwrite a file they could
// not back up, so /undo always has the original (final review I7).
func TestWriteIsRefusedWhenTheBackupFails(t *testing.T) {
	r, dir := fileRegistry(t)
	root := filepath.Join(dir, ".taracode")
	hm, err := history.NewManager(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	r.SetHistory(hm)
	// A regular file where the backups directory must go makes every backup fail.
	if err := os.WriteFile(filepath.Join(root, "backups"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"write_file", map[string]any{"path": "a.txt", "content": "replaced"}},
		{"edit_file", map[string]any{"path": "a.txt", "old": "two", "new": "2"}},
	}
	for _, c := range calls {
		_, err := r.Execute(context.Background(), c.tool, c.args, dir)
		if err == nil || !strings.Contains(err.Error(), "backup") {
			t.Errorf("%s: a failed backup must refuse the write: %v", c.tool, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("the file must be unchanged: %q", data)
	}
	if ops := hm.GetAllHistory(); len(ops) != 0 {
		t.Fatalf("a refused write records nothing: %+v", ops)
	}
	if _, err := r.Execute(context.Background(), "write_file", map[string]any{"path": "new.txt", "content": "x"}, dir); err != nil {
		t.Fatalf("a new file needs no backup and is written: %v", err)
	}
}
