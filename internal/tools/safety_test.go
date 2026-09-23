package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
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
