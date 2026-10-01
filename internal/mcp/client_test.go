package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClientTalksToAServer(t *testing.T) {
	t.Setenv("MCP_TEST_GREETING", "hello")
	client := startFake(t, "ok")
	if client.GetServerInfo() != nil {
		t.Fatal("no server info before the handshake")
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if info := client.GetServerInfo(); info == nil || info.Name != "fake hello" || info.Version != "1.2" {
		t.Fatalf("server info %+v: the env value is expanded before the server starts", info)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil || len(tools) != 2 || tools[0].Name != "list_issues" || tools[0].Annotations["readOnlyHint"] != true {
		t.Fatalf("tools %+v err=%v", tools, err)
	}
	res, err := client.CallTool(context.Background(), "list_issues", map[string]interface{}{"state": "open"})
	if err != nil || res.IsError || len(res.Content) != 3 || res.Content[0].Text != "called list_issues " ||
		res.Content[2].Text != `{"state":"open"}` {
		t.Fatalf("result %+v err=%v", res, err)
	}
	res, err = client.CallTool(context.Background(), "no_args", nil)
	if err != nil || res.Content[2].Text != "null" {
		t.Fatalf("a call without arguments sends none: %+v %v", res, err)
	}
	if _, err := client.CallTool(context.Background(), "rpc", nil); err == nil ||
		err.Error() != "tools/call failed: MCP error -32602: bad params" {
		t.Fatalf("a JSON-RPC error: %v", err)
	}
}

func TestClientSkipsLinesItCannotUse(t *testing.T) {
	client := startFake(t, "noisy")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tools, err := client.ListTools(context.Background()); err != nil || len(tools) != 2 {
		t.Fatalf("tools %+v err=%v", tools, err)
	}
}

// TestClientSendsTheInitializedNotification: a server that waits for notifications/initialized
// answers tools/list only after it, and Connect sends it.
func TestClientSendsTheInitializedNotification(t *testing.T) {
	client := startFake(t, "strict")
	if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "server not initialized") {
		t.Fatalf("the strict fake refuses tools/list before the handshake: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tools, err := client.ListTools(context.Background()); err != nil || len(tools) != 2 {
		t.Fatalf("tools %+v err=%v", tools, err)
	}
}

func TestClientReportsBadAnswers(t *testing.T) {
	tests := []struct {
		mode    string
		step    func(c *Client) error
		wantErr string
	}{
		{"init-error", func(c *Client) error { return c.Connect(context.Background()) },
			"initialize failed: MCP error -32000: not today"},
		{"bad-init", func(c *Client) error { return c.Connect(context.Background()) }, "failed to parse initialize response"},
		{"list-error", func(c *Client) error { _, err := c.ListTools(context.Background()); return err },
			"tools/list failed: MCP error -32601: no tools here"},
		{"bad-list", func(c *Client) error { _, err := c.ListTools(context.Background()); return err },
			"failed to parse tools/list response"},
		{"bad-call", func(c *Client) error { _, err := c.CallTool(context.Background(), "x", nil); return err },
			"failed to parse tools/call response"},
	}
	for _, tt := range tests {
		client := startFake(t, tt.mode)
		if err := tt.step(client); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want %q", tt.mode, err, tt.wantErr)
		}
	}
}

func TestClientCallEndsWithItsContext(t *testing.T) {
	client := startFake(t, "silent")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.CallTool(ctx, "x", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientAfterClose(t *testing.T) {
	client := startFake(t, "ok")
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal("a second close is a no-op")
	}
	if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "client is closed") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewClientReportsAServerThatCannotStart(t *testing.T) {
	if _, err := NewClient("/nonexistent/mcp-server", nil, nil, time.Second); err == nil ||
		!strings.Contains(err.Error(), "failed to start MCP server") {
		t.Fatalf("err = %v", err)
	}
}

func TestJSONRPCErrorMessage(t *testing.T) {
	if got := (&jsonRPCError{Code: -1, Message: "nope"}).Error(); got != "MCP error -1: nope" {
		t.Fatalf("%q", got)
	}
}

func TestClientReportsARequestItCannotSend(t *testing.T) {
	client := startFake(t, "ok")
	if _, err := client.CallTool(context.Background(), "x", map[string]interface{}{"c": make(chan int)}); err == nil ||
		!strings.Contains(err.Error(), "failed to marshal request") {
		t.Fatalf("arguments that are not JSON: %v", err)
	}
	if err := client.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to write request") {
		t.Fatalf("a server whose input is closed: %v", err)
	}
}
