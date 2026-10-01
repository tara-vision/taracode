package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
)

// dropToolSupport makes the fake's model a completion-only one, so any assistant built after this
// refuses it.
func dropToolSupport(srv *ollamatest.Server) {
	srv.Models[0].Capabilities = []string{"completion"}
}

// TestReloadKeepsTheModeTheHistoryAndTheMCPTools is the ruling P2-R18 regression for /reload: the
// fresh assistant gets the project's history manager, every connected MCP server's tools and the
// mode the session was in.
func TestReloadKeepsTheModeTheHistoryAndTheMCPTools(t *testing.T) {
	r, _ := projectREPL(t)
	r.mcp = newMCPManager(t, fakeMCPServer("fake", false))
	_ = captureStdoutForTest(t, func() { r.dispatch("/mcp connect fake") })
	if err := r.asst.SetMode(policy.ModeOperate); err != nil {
		t.Fatal(err)
	}
	before := r.asst
	out := captureStdoutForTest(t, func() { r.dispatch("/reload") })
	if r.asst == before || !strings.Contains(out, "Project context reloaded.") ||
		!strings.Contains(out, "No TARACODE.md yet; run /init to generate one.") {
		t.Fatalf("output %q", out)
	}
	if r.asst.Mode() != policy.ModeOperate {
		t.Fatalf("mode %s after /reload", r.asst.Mode())
	}
	registry := r.asst.ToolRegistry()
	if _, ok := registry.Get("fake.create_issue"); !ok {
		t.Fatal("the MCP tools were not registered into the new assistant")
	}
	if _, err := registry.Execute(context.Background(), "write_file", map[string]any{"path": "x.txt", "content": "hi"},
		r.projectRoot); err != nil {
		t.Fatal(err)
	}
	if ops := r.history.GetAllHistory(); len(ops) != 1 || ops[0].Tool != "write_file" {
		t.Fatalf("the new registry does not record into the history: %+v", ops)
	}
}

func TestReloadWithATaracodeMD(t *testing.T) {
	r, _ := projectREPL(t)
	writeFile(t, filepath.Join(r.projectRoot, "TARACODE.md"), "# Project\n")
	out := captureStdoutForTest(t, func() { r.dispatch("/reload") })
	if !strings.Contains(out, "Project context reloaded.") || strings.Contains(out, "No TARACODE.md") {
		t.Fatalf("%q", out)
	}
}

func TestReloadReportsAFailure(t *testing.T) {
	r, srv := projectREPL(t)
	dropToolSupport(srv)
	before := r.asst
	stdout, stderr := captureOutput(t, func() { r.dispatch("/reload") })
	if !strings.Contains(stderr, "Error reloading:") || !strings.Contains(stderr, "does not support tools") ||
		strings.Contains(stdout, "reloaded") || r.asst != before {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
}

// TestInitConnectsTheAutoConnectServers: MCP servers that connect on their own wait for /init in an
// uninitialised project.
func TestInitConnectsTheAutoConnectServers(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	r.mcp = newMCPManager(t, fakeMCPServer("fake", true))
	_ = captureStdoutForTest(t, func() { r.dispatch("/init") })
	if !r.initialised || !r.mcp.IsConnected("fake") {
		t.Fatalf("initialised %v, connected %v", r.initialised, r.mcp.IsConnected("fake"))
	}
}

func TestInitReportsAnUnwritableProject(t *testing.T) {
	skipIfRoot(t)
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	if err := os.Chmod(r.projectRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(r.projectRoot, 0o755) })
	_, stderr := captureOutput(t, func() { r.dispatch("/init") })
	if !strings.Contains(stderr, "Error: failed to initialize storage") || r.initialised {
		t.Fatalf("stderr %q, initialised %v", stderr, r.initialised)
	}
}

func TestInitReportsAFailedReconnect(t *testing.T) {
	srv := fakeOllama(t)
	r := replOn(t, srv, t.TempDir(), true)
	dropToolSupport(srv)
	_, stderr := captureOutput(t, func() { r.dispatch("/init") })
	if !strings.Contains(stderr, "Error reinitializing assistant:") || r.initialised {
		t.Fatalf("stderr %q, initialised %v", stderr, r.initialised)
	}
}
