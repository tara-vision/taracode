package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
)

func TestNewManager(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: []MCPServerConfig{
			{
				Name:        "test",
				Command:     "echo",
				Args:        []string{"hello"},
				AutoConnect: false,
				Timeout:     10 * time.Second,
			},
		},
	}

	mgr := NewManager(config)
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	servers := mgr.GetConfiguredServers()
	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}

	if servers[0].Name != "test" {
		t.Errorf("Expected server name 'test', got '%s'", servers[0].Name)
	}
}

func TestManagerNotConnected(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: []MCPServerConfig{
			{
				Name:    "test",
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}

	mgr := NewManager(config)

	if mgr.IsConnected("test") {
		t.Error("Expected not connected, but IsConnected returned true")
	}

	tools := mgr.GetAllTools()
	if len(tools) != 0 {
		t.Errorf("Expected 0 tools, got %d", len(tools))
	}
}

func TestManagerConnectUnknownServer(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: []MCPServerConfig{},
	}

	mgr := NewManager(config)

	err := mgr.Connect(nil, "unknown")
	if err == nil {
		t.Error("Expected error for unknown server, got nil")
	}
}

func TestMCPToolPrefixing(t *testing.T) {
	tool := MCPTool{
		Name:         "github.list_repos",
		ServerName:   "github",
		OriginalName: "list_repos",
		Description:  "List GitHub repositories",
	}

	if tool.Name != "github.list_repos" {
		t.Errorf("Expected prefixed name 'github.list_repos', got '%s'", tool.Name)
	}
}

func TestConnectionStatus(t *testing.T) {
	tests := []struct {
		status   ConnectionStatus
		expected string
	}{
		{StatusDisconnected, "disconnected"},
		{StatusConnecting, "connecting"},
		{StatusConnected, "connected"},
		{StatusError, "error"},
	}

	for _, tt := range tests {
		if tt.status.String() != tt.expected {
			t.Errorf("Expected status string '%s', got '%s'", tt.expected, tt.status.String())
		}
	}
}

func TestToToolClassifiesByTheReadOnlyHint(t *testing.T) {
	readTool := ToTool(nil, MCPTool{Name: "gh.list", ServerName: "gh", OriginalName: "list", ReadOnly: true}, true)
	if !readTool.ReadForm || readTool.Classify(nil, "").Classification != policy.Read {
		t.Fatalf("a readOnlyHint tool must be a read: %+v", readTool)
	}
	writeTool := ToTool(nil, MCPTool{Name: "gh.create", ServerName: "gh", OriginalName: "create"}, false)
	inv := writeTool.Classify(nil, "")
	if writeTool.ReadForm || inv.Classification != policy.Mutate || inv.Tool != "gh.create" {
		t.Fatalf("an unannotated tool must be a mutation hidden in investigate mode: %+v %+v", writeTool, inv)
	}
	if !strings.Contains(inv.Reason, "create on gh") {
		t.Fatalf("reason %q", inv.Reason)
	}
}

func TestParamsFromSchemaIsSortedAndTyped(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"title":  map[string]any{"type": "string", "description": "Issue title"},
			"number": map[string]any{"type": "integer"},
			"draft":  map[string]any{"type": "boolean"},
			"labels": map[string]any{"type": "array", "description": "Labels"},
			"weight": map[string]any{"type": "number"},
			"state":  map[string]any{"type": "string", "enum": []any{"open", "closed"}},
		},
		"required": []any{"title"},
	}
	params := paramsFromSchema(schema)
	var names []string
	for _, p := range params {
		names = append(names, p.Name+":"+p.Type)
	}
	if got := strings.Join(names, ","); got != "draft:boolean,labels:string,number:integer,state:string,title:string,weight:string" {
		t.Fatalf("params %s", got)
	}
	byName := map[string]tools.Param{}
	for _, p := range params {
		byName[p.Name] = p
	}
	if !byName["title"].Required || byName["state"].Required {
		t.Fatal("required flags")
	}
	if strings.Join(byName["state"].Enum, ",") != "open,closed" {
		t.Fatalf("enum %v", byName["state"].Enum)
	}
	if byName["labels"].Description != "Labels (JSON array)" || byName["weight"].Description != "(JSON number)" {
		t.Fatalf("JSON hints: %q %q", byName["labels"].Description, byName["weight"].Description)
	}
}

func TestDecodeJSONArgsTurnsJSONTextBackIntoValues(t *testing.T) {
	jsonArgs := jsonParams(map[string]any{"properties": map[string]any{
		"labels": map[string]any{"type": "array"},
		"weight": map[string]any{"type": "number"},
		"title":  map[string]any{"type": "string"},
	}})
	args := map[string]any{"labels": `["bug","ui"]`, "weight": "2.5", "title": `["kept as text"]`}
	got := decodeJSONArgs(args, jsonArgs)
	labels, ok := got["labels"].([]any)
	if !ok || len(labels) != 2 || labels[0] != "bug" {
		t.Fatalf("labels %#v", got["labels"])
	}
	if got["weight"] != 2.5 {
		t.Fatalf("weight %#v", got["weight"])
	}
	if got["title"] != `["kept as text"]` {
		t.Fatalf("a string param must stay text: %#v", got["title"])
	}
	if args["labels"] != `["bug","ui"]` {
		t.Fatal("the caller's args must not be modified")
	}
	if bad := decodeJSONArgs(map[string]any{"labels": "bug, ui"}, jsonArgs); bad["labels"] != "bug, ui" {
		t.Fatalf("text that is not JSON passes through: %#v", bad["labels"])
	}
}

func TestToToolRunCallsTheManager(t *testing.T) {
	tool := ToTool(NewManager(MCPConfig{}), MCPTool{Name: "gh.list", ServerName: "gh", OriginalName: "list"}, false)
	if _, err := tool.Run(context.Background(), map[string]any{}, ""); err == nil || !strings.Contains(err.Error(), "unknown MCP tool: gh.list") {
		t.Fatalf("a tool of a server that is not connected: %v", err)
	}
}
