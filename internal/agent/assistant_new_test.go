package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// newOptions is a New configuration on a fake serving gemma4:12b, with HOME isolated so no global
// policy is read, and output collected in the returned buffer.
func newOptions(t *testing.T, dir string) (Options, *ollamatest.Server, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}
	var out bytes.Buffer
	opts := DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir, opts.Spinner, opts.Output = srv.URL, "gemma4:12b", dir, false, &out
	return opts, srv, &out
}

func writeProjectFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewNeedsAHost(t *testing.T) {
	if _, err := New(Options{}); err == nil || !strings.Contains(err.Error(), "failed to create provider: host is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewCapsIterationsAndWarnsAboutAnUnknownThink(t *testing.T) {
	opts, _, out := newOptions(t, t.TempDir())
	opts.Ephemeral, opts.MaxIterations, opts.Think = true, 500, "sideways"
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.maxIterations != 50 || a.Think() != "" {
		t.Fatalf("iterations %d, think %q", a.maxIterations, a.Think())
	}
	if !strings.Contains(out.String(), `think "sideways" not recognized; using auto`) {
		t.Fatalf("%q", out.String())
	}
}

func TestNewWithoutWritableStorageRunsWithoutPersistence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	opts, _, out := newOptions(t, dir)
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.GetStorage() != nil || a.GetSession() != nil || !strings.Contains(out.String(), "Could not initialize storage") {
		t.Fatalf("storage %v, output %q", a.GetStorage(), out.String())
	}
}

// TestNewReportsWhereThePolicyAndTheStorageAre: a project policy file is the policy's one source,
// and the project's storage lives in its .taracode directory.
func TestNewReportsWhereThePolicyAndTheStorageAre(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, ".taracode/policy.yaml", "version: 1\n")
	opts, _, _ := newOptions(t, dir)
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if sources := a.PolicySources(); len(sources) != 1 || sources[0] != filepath.Join(dir, ".taracode", "policy.yaml") ||
		a.PolicyError() != nil {
		t.Fatalf("sources %v, error %v", sources, a.PolicyError())
	}
	if st := a.GetStorage(); st == nil || st.GetRootDir() != filepath.Join(dir, ".taracode") {
		t.Fatalf("storage %+v", st)
	}
}

func TestNewLocksABrokenPolicyToInvestigate(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, ".taracode/policy.yaml", "version: 9\n")
	opts, _, out := newOptions(t, dir)
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if a.PolicyError() == nil || a.PolicySources() != nil ||
		!strings.Contains(out.String(), "Policy error, session locked to investigate mode") {
		t.Fatalf("policy error %v, sources %v, output %q", a.PolicyError(), a.PolicySources(), out.String())
	}
	if a.Policy().Version == 0 || len(a.Policy().Protected.KubeNamespaces) == 0 {
		t.Fatalf("the built-in policy stands in: %+v", a.Policy())
	}
}

func TestNewReportsAPermissionsFileItCannotUse(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"corrupt", "{not json", "Permissions file ignored:"},
		{"2.x", `{"version": 2, "categories": {"file_write": "allow"}}`, "permissions.json is from taracode 2.x and was ignored"},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		writeProjectFile(t, dir, ".taracode/permissions.json", tt.content)
		opts, _, out := newOptions(t, dir)
		a, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), tt.want) || a.Permissions() == nil || len(a.Permissions().Rules()) != 0 {
			t.Errorf("%s: output %q, permissions %v", tt.name, out.String(), a.Permissions())
		}
	}
}

func TestNewIgnoresABadRedactionPattern(t *testing.T) {
	dir := t.TempDir()
	writeProjectFile(t, dir, ".taracode/policy.yaml", "version: 1\nredact:\n  extra_patterns: [\"(unclosed\"]\n")
	opts, _, out := newOptions(t, dir)
	if _, err := New(opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Redaction extra pattern ignored") {
		t.Fatalf("%q", out.String())
	}
}
