package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeServerEnv selects how the fake behaves; the child process the tests start is the test binary
// itself, running TestFakeServerProcess as a stdio MCP server.
const fakeServerEnv = "taracode_mcp_fake_mode"

// fakeExitMarkerEnv names a file the fake creates once its input has closed, just before it exits.
const fakeExitMarkerEnv = "taracode_mcp_fake_exit_marker"

// TestFakeServerProcess is not a test: when the environment asks for it, the test binary serves MCP
// over stdin and stdout until stdin closes.
func TestFakeServerProcess(_ *testing.T) {
	mode := os.Getenv(fakeServerEnv)
	if mode == "" {
		return
	}
	serveFake(mode, os.Stdin, os.Stdout)
	if marker := os.Getenv(fakeExitMarkerEnv); marker != "" {
		_ = os.WriteFile(marker, nil, 0o600)
	}
	os.Exit(0)
}

// fakeTools is what the fake lists.
var fakeTools = []map[string]any{
	{"name": "list_issues", "description": "List issues", "annotations": map[string]any{"readOnlyHint": true},
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"labels": map[string]any{"type": "array"}}}},
	{"name": "create_issue", "description": "Create an issue", "inputSchema": map[string]any{"type": "object"}},
}

// serveFake answers one JSON-RPC request per line. Modes break one step: init-error, bad-init,
// list-error, bad-list, bad-call, silent, die-on-call; noisy writes junk lines before each answer;
// strict refuses every request but initialize until the initialized notification has come.
func serveFake(mode string, in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	initialized := false
	for scanner.Scan() {
		var req struct {
			ID     *int64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			continue
		}
		if req.ID == nil {
			initialized = initialized || req.Method == "notifications/initialized"
			continue
		}
		if mode == "noisy" {
			_, _ = fmt.Fprint(out, "\nnot json\n{\"jsonrpc\":\"2.0\",\"id\":999,\"result\":{}}\n")
		}
		result, rpcErr := fakeAnswer(mode, req.Method, req.Params)
		if mode == "strict" && !initialized && req.Method != "initialize" {
			result, rpcErr = nil, map[string]any{"code": -32002, "message": "server not initialized"}
		}
		switch {
		case result == nil && rpcErr == nil && mode == "silent":
			continue
		case result == nil && rpcErr == nil && mode == "die-on-call":
			os.Exit(0)
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		data, _ := json.Marshal(resp)
		_, _ = fmt.Fprintf(out, "%s\n", data)
	}
}

// fakeAnswer is the result (or the JSON-RPC error) for one method in one mode; both nil means no
// answer at all.
func fakeAnswer(mode, method string, params map[string]any) (any, any) {
	switch method {
	case "initialize":
		switch mode {
		case "init-error":
			return nil, map[string]any{"code": -32000, "message": "not today"}
		case "bad-init":
			return "a string, not an object", nil
		}
		return map[string]any{"protocolVersion": "2024-11-05",
			"serverInfo": map[string]any{"name": "fake " + os.Getenv("taracode_mcp_greeting"), "version": "1.2"}}, nil
	case "tools/list":
		switch mode {
		case "list-error":
			return nil, map[string]any{"code": -32601, "message": "no tools here"}
		case "bad-list":
			return map[string]any{"tools": "none"}, nil
		}
		return map[string]any{"tools": fakeTools}, nil
	}
	switch mode {
	case "bad-call":
		return []int{1}, nil
	case "silent", "die-on-call":
		return nil, nil
	}
	name, _ := params["name"].(string)
	args, _ := json.Marshal(params["arguments"])
	switch name {
	case "fails":
		return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "it broke"}}}, nil
	case "rpc":
		return nil, map[string]any{"code": -32602, "message": "bad params"}
	}
	return map[string]any{"content": []map[string]any{
		{"type": "text", "text": "called " + name + " "}, {"type": "image"}, {"type": "text", "text": string(args)}}}, nil
}

// fakeConfig is a server config that starts the fake in mode. Under -race the child would sleep a
// second at exit before it lets Close return; GORACE turns that pause off.
func fakeConfig(name, mode string) MCPServerConfig {
	return MCPServerConfig{Name: name, Command: os.Args[0], Args: []string{"-test.run=^TestFakeServerProcess$"},
		Env: map[string]string{fakeServerEnv: mode, "taracode_mcp_greeting": "${MCP_TEST_GREETING}",
			"GORACE": strings.TrimSpace(os.Getenv("GORACE") + " atexit_sleep_ms=0")}, Timeout: 20 * time.Second}
}

// startFake starts a client on the fake in mode and closes it when the test ends.
func startFake(t *testing.T, mode string) *Client {
	t.Helper()
	cfg := fakeConfig("fake", mode)
	client, err := NewClient(cfg.Command, cfg.Args, cfg.Env, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
