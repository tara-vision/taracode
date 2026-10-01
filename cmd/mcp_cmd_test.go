package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/mcp"
)

// fakeMCPEnv is set on the child process the MCP tests start: the test binary itself, running
// TestFakeMCPServerProcess as a stdio MCP server. It is lower case because a config read through
// viper lower-cases every key, env names included.
const fakeMCPEnv = "taracode_fake_mcp_server"

// TestFakeMCPServerProcess is not a test: when the environment asks for it, the test binary serves
// MCP over stdin and stdout until stdin closes, so the MCP tests need no external server.
func TestFakeMCPServerProcess(_ *testing.T) {
	if os.Getenv(fakeMCPEnv) != "1" {
		return
	}
	serveFakeMCP(os.Stdin, os.Stdout)
	os.Exit(0)
}

// fakeMCPTools are the two tools the fake lists: one annotated read-only, one not.
var fakeMCPTools = []map[string]any{
	{"name": "list_issues", "description": "List the issues of a repository, with every filter the API has",
		"inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}},
	{"name": "create_issue", "description": "Create an issue", "inputSchema": map[string]any{"type": "object"}},
}

// serveFakeMCP answers initialize, tools/list and tools/call, one JSON-RPC line per request.
func serveFakeMCP(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.ID == nil {
			continue // a notification gets no answer
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "fake", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": fakeMCPTools}
		default:
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "called " + req.Method}}}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
		_, _ = fmt.Fprintf(out, "%s\n", data)
	}
}

// fakeMCPServer is the config of a server that runs the fake.
func fakeMCPServer(name string, autoConnect bool) mcp.MCPServerConfig {
	return mcp.MCPServerConfig{
		Name: name, Command: os.Args[0], Args: []string{"-test.run=^TestFakeMCPServerProcess$"},
		Env:         map[string]string{fakeMCPEnv: "1", "GORACE": strings.TrimSpace(os.Getenv("GORACE") + " atexit_sleep_ms=0")},
		AutoConnect: autoConnect, Timeout: 20 * time.Second,
	}
}

// newMCPManager builds a manager for the servers and closes every connection when the test ends.
func newMCPManager(t *testing.T, servers ...mcp.MCPServerConfig) *mcp.Manager {
	t.Helper()
	mgr := mcp.NewManager(mcp.MCPConfig{Enabled: true, Servers: servers})
	t.Cleanup(mgr.Close)
	return mgr
}

func TestMCPWithoutAManagerExplainsTheConfig(t *testing.T) {
	out := captureStdoutForTest(t, func() { handleMCP(nil, nil, nil) })
	for _, want := range []string{"MCP is not enabled.", "mcp:", "enabled: true", "command: npx"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestFormatMCPStatus(t *testing.T) {
	tests := []struct{ status, want string }{
		{"connected", "\033[32mconnected\033[0m"},
		{"connecting", "\033[33mconnecting\033[0m"},
		{"error", "\033[31merror\033[0m"},
		{"disconnected", "\033[90mdisconnected\033[0m"},
	}
	for _, tt := range tests {
		if got := formatMCPStatus(tt.status); got != tt.want {
			t.Errorf("formatMCPStatus(%s) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestMCPListsTheServers(t *testing.T) {
	out := captureStdoutForTest(t, func() { handleMCP(newMCPManager(t), nil, nil) })
	if !strings.Contains(out, "No MCP servers configured.") {
		t.Fatalf("%q", out)
	}

	broken := mcp.MCPServerConfig{Name: "broken", Command: "/nonexistent/mcp-server", Timeout: time.Second}
	mgr := newMCPManager(t, fakeMCPServer("fake", true), broken, mcp.MCPServerConfig{Name: "idle", Command: "true"})
	r, _ := projectREPL(t)
	_ = captureStdoutForTest(t, func() { handleMCP(mgr, []string{"connect", "fake"}, r.asst) })
	_ = captureStdoutForTest(t, func() { handleMCP(mgr, []string{"connect", "broken"}, r.asst) })
	out = captureStdoutForTest(t, func() { handleMCP(mgr, nil, r.asst) })
	for _, want := range []string{
		"MCP Servers:", "  fake            \033[32mconnected\033[0m (auto-connect)  [2 tools]",
		"  broken          \033[31merror\033[0m\n", "  idle            \033[90mdisconnected\033[0m\n",
		"/mcp connect <name>", "/mcp disconnect <name>", "/mcp tools",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/mcp lacks %q:\n%s", want, out)
		}
	}
}

func TestMCPConnectRegistersTheTools(t *testing.T) {
	r, _ := projectREPL(t)
	r.mcp = newMCPManager(t, fakeMCPServer("fake", false))
	out := captureStdoutForTest(t, func() { r.dispatch("/mcp connect") })
	if !strings.Contains(out, "Usage: /mcp connect <server-name>") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/mcp connect nope") })
	if !strings.Contains(out, "Connecting to nope...") || !strings.Contains(out, "Error: unknown MCP server: nope") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/mcp connect fake") })
	if !strings.Contains(out, "Connected to fake. Discovered 2 tools.") {
		t.Fatalf("%q", out)
	}
	registry := r.asst.ToolRegistry()
	readTool, okRead := registry.Get("fake.list_issues")
	writeTool, okWrite := registry.Get("fake.create_issue")
	if !okRead || !okWrite || !readTool.ReadForm || writeTool.ReadForm {
		t.Fatalf("registered: %v %v, read forms %+v %+v", okRead, okWrite, readTool, writeTool)
	}
	if got := registry.MCPTools()["fake"]; strings.Join(got, ",") != "fake.list_issues,fake.create_issue" {
		t.Fatalf("MCP tools of fake: %v", got)
	}
}

func TestMCPToolsAndDisconnect(t *testing.T) {
	r, _ := projectREPL(t)
	r.mcp = newMCPManager(t, fakeMCPServer("fake", false))
	out := captureStdoutForTest(t, func() { r.dispatch("/mcp tools") })
	if !strings.Contains(out, "No MCP tools available. Connect to a server first.") {
		t.Fatalf("%q", out)
	}
	_ = captureStdoutForTest(t, func() { r.dispatch("/mcp connect fake") })
	out = captureStdoutForTest(t, func() { r.dispatch("/mcp tools") })
	long, _ := fakeMCPTools[0]["description"].(string)
	for _, want := range []string{
		"MCP Tools (2 total):", "  fake:\n",
		fmt.Sprintf("    %-30s %s...\n", "fake.list_issues", long[:47]),
		fmt.Sprintf("    %-30s %s\n", "fake.create_issue", "Create an issue"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/mcp tools lacks %q:\n%s", want, out)
		}
	}

	out = captureStdoutForTest(t, func() { r.dispatch("/mcp disconnect") })
	if !strings.Contains(out, "Usage: /mcp disconnect <server-name>") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/mcp disconnect fake") })
	if !strings.Contains(out, "Disconnected from fake.") {
		t.Fatalf("%q", out)
	}
	if _, ok := r.asst.ToolRegistry().Get("fake.list_issues"); ok {
		t.Fatal("a disconnected server's tools stay registered")
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/mcp disconnect fake") })
	if !strings.Contains(out, "Error: not connected to fake") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/mcp frobnicate") })
	if !strings.Contains(out, "Unknown subcommand: frobnicate") || !strings.Contains(out, "Usage: /mcp [connect|disconnect|tools]") {
		t.Fatalf("%q", out)
	}
}
