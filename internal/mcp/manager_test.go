package mcp

import (
	"testing"
	"time"
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
