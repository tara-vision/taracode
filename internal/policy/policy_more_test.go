package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheModeDenialNamesWhatWasAsked: without a command line the denial names the paths, and without
// either it names the tool.
func TestTheModeDenialNamesWhatWasAsked(t *testing.T) {
	p := Default()
	tests := []struct {
		inv  Invocation
		want string
	}{
		{mutate("write_file", "", "", Targets{Paths: []string{"/w/a.txt", "/w/b.txt"}}), "; write_file /w/a.txt, /w/b.txt was classified"},
		{mutate("edit_file", "", "", Targets{}), "; edit_file was classified"},
	}
	for _, tt := range tests {
		if v := p.Evaluate(ModeInvestigate, tt.inv); v.Allow || !strings.Contains(v.Reason, tt.want) {
			t.Errorf("%+v, want %q", v, tt.want)
		}
	}
}

// TestDenyPatternsReadOnlyCommandLines: a mutation without a command line (a file tool) is never
// matched against deny.commands.
func TestDenyPatternsReadOnlyCommandLines(t *testing.T) {
	p := Default()
	p.Deny.Commands = []string{"*"}
	if v := p.Evaluate(ModeOperate, mutate("write_file", "", "", Targets{Paths: []string{"/w/notes.txt"}})); !v.Allow ||
		v.Rule != "policy" {
		t.Fatalf("%+v", v)
	}
	if v := p.Evaluate(ModeOperate, mutate("shell", "", "make deploy", Targets{})); v.Allow || v.Rule != "deny.commands" {
		t.Fatalf("%+v", v)
	}
}

func TestPathGlobQuestionMarkStaysInOneName(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/w/secret1.key", true}, {"/w/secret12.key", false}, {"/w/secret/.key", false},
	}
	for _, tt := range tests {
		if got := matchPath("secret?.key", tt.path, "/w"); got != tt.want {
			t.Errorf("matchPath(secret?.key, %s) = %v", tt.path, got)
		}
	}
}

// TestPathGlobLeadingDoubleStarMatchesNoDirectory: in path mode a leading **/ also matches a path
// with no directory in front, as globRegexp's comment says.
func TestPathGlobLeadingDoubleStarMatchesNoDirectory(t *testing.T) {
	re, err := globRegexp("**/secret.key", true, false)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{"secret.key": true, "a/b/secret.key": true, "a/notsecret.key": false} {
		if got := re.MatchString(path); got != want {
			t.Errorf("**/secret.key against %s: %v", path, got)
		}
	}
}

func TestPermissionsEdges(t *testing.T) {
	if perm, ok := ParsePermission(" Ask "); !ok || perm != Ask {
		t.Fatalf("%q %v", perm, ok)
	}
	if _, _, err := LoadPermissions(t.TempDir()); err == nil {
		t.Fatal("a directory is no permissions file")
	}
	mem := AllowAll()
	if err := mem.Set("kubectl", Deny); err != nil || mem.For("kubectl") != Deny || mem.For("git") != Allow {
		t.Fatalf("an in-memory store keeps its rules without a file: %v", err)
	}
	rules := mem.Rules()
	rules["git"] = Deny
	if mem.For("git") != Allow || len(mem.Rules()) != 2 {
		t.Fatalf("Rules hands out a copy: %v", mem.Rules())
	}
	dir := t.TempDir()
	p, _, err := LoadPermissions(filepath.Join(dir, "taracode", "permissions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taracode"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("kubectl", Allow); err == nil {
		t.Fatal("a permissions file under a file cannot be saved")
	}
}

func TestMergeKeepsTheNewerVersionAndAnUnsetTrust(t *testing.T) {
	off := false
	got := Merge(Policy{Version: 1, MCP: MCPRules{TrustReadOnlyHint: &off}}, Policy{Version: 2})
	if got.Version != 2 || got.MCP.TrustReadOnlyHint == nil || *got.MCP.TrustReadOnlyHint {
		t.Fatalf("%+v", got)
	}
}

func TestLoadReportsAPolicyFileItCannotRead(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".taracode", "policy.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(project, t.TempDir()); err == nil || !strings.HasPrefix(err.Error(), "read ") {
		t.Fatalf("err = %v", err)
	}
}
