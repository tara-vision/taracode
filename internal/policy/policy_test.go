package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsUnknownKeysAndBadValues(t *testing.T) {
	cases := map[string]string{
		"unknown key": "version: 1\nprotected:\n  kube_context: [\"x\"]\n",
		"bad version": "version: 7\n",
		"bad mode":    "version: 1\nmode: yolo\n",
		"empty":       "",
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	p, err := Parse([]byte("version: 1\nmode: operate\nprotected:\n  paths: [\"**/*.tfstate\"]\nrequire_dry_run:\n  kubectl_apply: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != ModeOperate || len(p.Protected.Paths) != 1 || p.RequireDryRun.KubectlApply == nil || *p.RequireDryRun.KubectlApply {
		t.Fatalf("parsed %+v", p)
	}
}

func TestMergeUnionsListsAndKeepsTheStricterBoolean(t *testing.T) {
	f, tr := false, true
	base := Policy{Version: 1, Mode: ModeInvestigate,
		Protected:     Protected{KubeContexts: []string{"*prod*"}, Paths: []string{".git/**"}},
		RequireDryRun: RequireDryRun{KubectlApply: &tr, HelmUpgrade: &f},
		Redact:        Redact{Enabled: &f}}
	over := Policy{Version: 1, Mode: ModeOperate,
		Protected:     Protected{KubeContexts: []string{"*prod*", "*staging*"}},
		RequireDryRun: RequireDryRun{KubectlApply: &f, HelmUpgrade: &tr},
		Redact:        Redact{Enabled: &tr, ExtraPatterns: []string{"ACME-[0-9]+"}}}
	m := Merge(base, over)
	if m.Mode != ModeOperate {
		t.Errorf("mode %q", m.Mode)
	}
	if strings.Join(m.Protected.KubeContexts, ",") != "*prod*,*staging*" {
		t.Errorf("contexts %v", m.Protected.KubeContexts)
	}
	if len(m.Protected.Paths) != 1 {
		t.Errorf("paths %v", m.Protected.Paths)
	}
	if !*m.RequireDryRun.KubectlApply || !*m.RequireDryRun.HelmUpgrade || m.RequireDryRun.TerraformApply != nil {
		t.Errorf("dry runs %+v", m.RequireDryRun)
	}
	if !*m.Redact.Enabled || len(m.Redact.ExtraPatterns) != 1 {
		t.Errorf("redact %+v", m.Redact)
	}
}

func TestLoadWithoutFilesIsTheBuiltInPolicy(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	p, sources, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0] != "built-in" {
		t.Errorf("sources %v", sources)
	}
	d := Default()
	if strings.Join(p.Deny.Commands, "|") != strings.Join(d.Deny.Commands, "|") || !p.RedactEnabled() {
		t.Errorf("not the default policy: %+v", p)
	}
	global, proj := Files(project, home)
	for _, want := range []string{global, proj} {
		if !contains(p.Protected.Paths, want) {
			t.Errorf("policy file %s is not protected: %v", want, p.Protected.Paths)
		}
	}
}

func TestLoadMergesGlobalThenProjectAndNamesABrokenFile(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	global, proj := Files(project, home)
	write(t, global, "version: 1\nmode: investigate\nprotected:\n  kube_namespaces: [\"kube-system\"]\n")
	write(t, proj, "version: 1\nmode: operate\nprotected:\n  kube_namespaces: [\"monitoring\"]\n")
	p, sources, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || p.Mode != ModeOperate || strings.Join(p.Protected.KubeNamespaces, ",") != "kube-system,monitoring" {
		t.Fatalf("merged %+v from %v", p, sources)
	}
	if len(p.Deny.Commands) != 0 {
		t.Errorf("a policy file must replace the built-in policy, got deny %v", p.Deny.Commands)
	}
	write(t, proj, "version: 1\nprotected:\n  pths: []\n")
	if _, _, err := Load(project, home); err == nil || !strings.Contains(err.Error(), proj) {
		t.Fatalf("expected an error naming %s, got %v", proj, err)
	}
}

func TestGlobs(t *testing.T) {
	cases := []struct {
		pattern, value string
		want           bool
	}{
		{"*prod*", "gke-prod-eu", true}, {"*prod*", "PRODUCTION", true}, {"*prod*", "staging", false},
		{"kube-system", "kube-system", true}, {"kube-?", "kube-x", true}, {"123456789012", "arn:aws:iam::123456789012:role/x", false},
		{"*123456789012*", "arn:aws:iam::123456789012:role/x", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.value); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v", c.pattern, c.value, got)
		}
	}
	paths := []struct {
		pattern, abs, dir string
		want              bool
	}{
		{"**/*.tfstate", "/w/infra/terraform.tfstate", "/w", true},
		{"**/*.tfstate", "/w/terraform.tfstate", "/w", true},
		{"*.tfstate", "/w/infra/prod.tfstate", "/w", true},
		{".git/**", "/w/.git/config", "/w", true},
		{".git/**", "/w/src/.git/config", "/w", false},
		{"infra/*", "/w/infra/main.tf", "/w", true},
		{"infra/*", "/w/infra/modules/vpc.tf", "/w", false},
		{"/etc/hosts", "/etc/hosts", "/w", true},
	}
	for _, c := range paths {
		if got := matchPath(c.pattern, c.abs, c.dir); got != c.want {
			t.Errorf("matchPath(%q, %q, %q) = %v", c.pattern, c.abs, c.dir, got)
		}
	}
}

func TestStarterParsesToTheDefaultPolicy(t *testing.T) {
	p, err := Parse([]byte(StarterYAML))
	if err != nil {
		t.Fatal(err)
	}
	d := Default()
	if strings.Join(p.Protected.KubeContexts, ",") != strings.Join(d.Protected.KubeContexts, ",") ||
		strings.Join(p.Deny.Commands, "|") != strings.Join(d.Deny.Commands, "|") || p.Mode != d.Mode {
		t.Fatalf("starter %+v differs from default %+v", p, d)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestMCPRulesParseMergeAndDecide(t *testing.T) {
	p, err := Parse([]byte("version: 1\nmcp:\n  trust_read_only_hint: false\n  read_only:\n    github: [\"get_*\", \"list_*\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if enabled(p.MCP.TrustReadOnlyHint) || len(p.MCP.ReadOnly["github"]) != 2 {
		t.Fatalf("parsed %+v", p.MCP)
	}
	if !Default().MCPReadOnly("github", "get_issue", true) || Default().MCPReadOnly("github", "get_issue", false) {
		t.Fatal("the default trusts the hint")
	}
	if !p.MCPReadOnly("github", "get_issue", false) || p.MCPReadOnly("github", "create_issue", true) ||
		p.MCPReadOnly("other", "get_issue", true) {
		t.Fatalf("distrusting policy decided wrongly")
	}
	m := Merge(Default(), p)
	if enabled(m.MCP.TrustReadOnlyHint) || strings.Join(m.MCP.ReadOnly["github"], ",") != "get_*,list_*" {
		t.Fatalf("merged %+v", m.MCP)
	}
	if starter, err := Parse([]byte(StarterYAML)); err != nil || !enabled(starter.MCP.TrustReadOnlyHint) {
		t.Fatalf("starter %+v %v", starter.MCP, err)
	}
}
