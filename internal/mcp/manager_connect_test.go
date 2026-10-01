package mcp

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestManager is a manager on servers whose connections close when the test ends.
func newTestManager(t *testing.T, servers ...MCPServerConfig) *Manager {
	t.Helper()
	mgr := NewManager(MCPConfig{Enabled: true, Servers: servers})
	t.Cleanup(mgr.Close)
	return mgr
}

func TestManagerConnectDiscoversTheTools(t *testing.T) {
	mgr := newTestManager(t, fakeConfig("gh", "ok"))
	var mu sync.Mutex
	var discovered []string
	mgr.SetToolDiscoveryCallback(func(server string, tools []MCPTool) {
		mu.Lock()
		defer mu.Unlock()
		for _, tool := range tools {
			discovered = append(discovered, server+":"+tool.Name)
		}
	})
	if err := mgr.Connect(context.Background(), "gh"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := strings.Join(discovered, ",")
	mu.Unlock()
	if got != "gh:gh.list_issues,gh:gh.create_issue" {
		t.Fatalf("the callback saw %s", got)
	}
	tools := mgr.GetToolsByServer("gh")
	if len(tools) != 2 || tools[0].OriginalName != "list_issues" || tools[0].ServerName != "gh" || !tools[0].ReadOnly ||
		tools[1].ReadOnly || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools %+v", tools)
	}
	conn := mgr.GetConnection("gh")
	if conn == nil || conn.Status != StatusConnected || conn.Client == nil || conn.ConnectedAt.IsZero() || conn.Error != nil {
		t.Fatalf("connection %+v", conn)
	}
	if all := mgr.GetAllConnections(); len(all) != 1 || all["gh"] != conn {
		t.Fatalf("connections %+v", all)
	}
	if err := mgr.Connect(context.Background(), "gh"); err == nil || err.Error() != "already connected to gh" {
		t.Fatalf("a second connect: %v", err)
	}
	if mgr.GetToolsByServer("other") != nil {
		t.Fatal("an unknown server has no tools")
	}
}

func TestManagerRecordsAFailedConnection(t *testing.T) {
	tests := []struct {
		cfg     MCPServerConfig
		wantErr string
	}{
		{MCPServerConfig{Name: "missing", Command: "/nonexistent/server"}, "failed to create MCP client"},
		{fakeConfig("refuses", "init-error"), "failed to connect: initialize failed"},
		{fakeConfig("toolless", "list-error"), "failed to list tools: tools/list failed"},
	}
	for _, tt := range tests {
		mgr := newTestManager(t, tt.cfg)
		err := mgr.Connect(context.Background(), tt.cfg.Name)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want %q", tt.cfg.Name, err, tt.wantErr)
			continue
		}
		conn := mgr.GetConnection(tt.cfg.Name)
		if conn.Status != StatusError || conn.Error == nil || mgr.IsConnected(tt.cfg.Name) {
			t.Errorf("%s: connection %+v", tt.cfg.Name, conn)
		}
		if err := mgr.Disconnect(tt.cfg.Name); err == nil || err.Error() != "not connected to "+tt.cfg.Name {
			t.Errorf("%s: disconnect of a failed connection: %v", tt.cfg.Name, err)
		}
	}
}

// TestManagerStopsAServerItCouldNotUse: a failed handshake or tool listing closes the client, so
// the server sees its input end and exits before Connect returns.
func TestManagerStopsAServerItCouldNotUse(t *testing.T) {
	for _, mode := range []string{"init-error", "list-error"} {
		marker := filepath.Join(t.TempDir(), "exited")
		cfg := fakeConfig("fake", mode)
		cfg.Env[fakeExitMarkerEnv] = marker
		if err := newTestManager(t, cfg).Connect(context.Background(), "fake"); err == nil {
			t.Fatalf("%s: the connection should fail", mode)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("%s: the server is still running after Connect returned: %v", mode, err)
		}
	}
}

func TestManagerDisconnect(t *testing.T) {
	mgr := newTestManager(t, fakeConfig("gh", "ok"))
	if err := mgr.Disconnect("gh"); err == nil || err.Error() != "not connected to gh" {
		t.Fatalf("never connected: %v", err)
	}
	if err := mgr.Connect(context.Background(), "gh"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Disconnect("gh"); err != nil {
		t.Fatal(err)
	}
	conn := mgr.GetConnection("gh")
	if conn.Status != StatusDisconnected || conn.Client != nil || conn.Tools != nil || mgr.IsConnected("gh") ||
		len(mgr.GetAllTools()) != 0 {
		t.Fatalf("after disconnect %+v", conn)
	}
	if err := mgr.Connect(context.Background(), "gh"); err != nil {
		t.Fatalf("a disconnected server connects again: %v", err)
	}
}

func TestManagerCallTool(t *testing.T) {
	mgr := newTestManager(t, fakeConfig("gh", "ok"), fakeConfig("other", "ok"))
	for _, name := range []string{"gh", "other"} {
		if err := mgr.Connect(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	out, err := mgr.CallTool(context.Background(), "gh.create_issue", map[string]interface{}{"title": "x"})
	if err != nil || out != `called create_issue {"title":"x"}` {
		t.Fatalf("CallTool() = %q, %v: the text parts joined, the rest skipped", out, err)
	}
	if _, err := mgr.CallTool(context.Background(), "gh.fails", nil); err == nil {
		t.Fatal("unknown names are not routed")
	}
	if _, err := mgr.CallTool(context.Background(), "gh.nope", nil); err == nil || err.Error() != "unknown MCP tool: gh.nope" {
		t.Fatalf("err = %v", err)
	}

	// Tool errors and protocol errors, through tools the fake reports for any name.
	conn := mgr.GetConnection("gh")
	mgr.mu.Lock()
	conn.Tools = append(conn.Tools, MCPTool{Name: "gh.fails", ServerName: "gh", OriginalName: "fails"},
		MCPTool{Name: "gh.rpc", ServerName: "gh", OriginalName: "rpc"})
	mgr.mu.Unlock()
	if _, err := mgr.CallTool(context.Background(), "gh.fails", nil); err == nil || err.Error() != "tool error: it broke" {
		t.Fatalf("a tool error: %v", err)
	}
	if _, err := mgr.CallTool(context.Background(), "gh.rpc", nil); err == nil || !strings.Contains(err.Error(), "bad params") {
		t.Fatalf("a protocol error: %v", err)
	}
}

func TestManagerCallToolSkipsDisconnectedServers(t *testing.T) {
	mgr := newTestManager(t, fakeConfig("gh", "ok"))
	if err := mgr.Connect(context.Background(), "gh"); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	mgr.connections["stale"] = &MCPConnection{Status: StatusError, Tools: []MCPTool{{Name: "stale.x"}}}
	mgr.mu.Unlock()
	if _, err := mgr.CallTool(context.Background(), "stale.x", nil); err == nil || err.Error() != "unknown MCP tool: stale.x" {
		t.Fatalf("err = %v", err)
	}
	if tools := mgr.GetAllTools(); len(tools) != 2 {
		t.Fatalf("only connected servers' tools: %+v", tools)
	}
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns everything it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()
	func() {
		defer func() { os.Stdout = original }() // also when fn fails the test
		fn()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

func TestManagerAutoConnect(t *testing.T) {
	auto := fakeConfig("auto", "ok")
	auto.AutoConnect = true
	broken := MCPServerConfig{Name: "broken", Command: "/nonexistent/server", AutoConnect: true, Timeout: time.Second}
	manual := fakeConfig("manual", "ok")
	mgr := newTestManager(t, auto, broken, manual)
	out := captureStdout(t, func() { mgr.AutoConnect(context.Background()) })
	if !mgr.IsConnected("auto") || mgr.IsConnected("manual") || mgr.IsConnected("broken") {
		t.Fatalf("connections %+v", mgr.GetAllConnections())
	}
	if !strings.Contains(out, "Warning: Failed to auto-connect to MCP server 'broken'") {
		t.Fatalf("%q", out)
	}
	mgr.Close()
	if len(mgr.GetAllConnections()) != 0 || mgr.IsConnected("auto") {
		t.Fatal("Close drops every connection")
	}
}
