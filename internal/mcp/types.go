package mcp

import (
	"time"
)

// MCPServerConfig represents configuration for an MCP server
type MCPServerConfig struct {
	Name        string            `mapstructure:"name"`
	Command     string            `mapstructure:"command"`
	Args        []string          `mapstructure:"args"`
	Env         map[string]string `mapstructure:"env"`
	AutoConnect bool              `mapstructure:"auto_connect"`
	Timeout     time.Duration     `mapstructure:"timeout"`
}

// MCPConfig represents the overall MCP configuration
type MCPConfig struct {
	Enabled bool              `mapstructure:"enabled"`
	Servers []MCPServerConfig `mapstructure:"servers"`
}

// MCPTool represents a tool discovered from an MCP server
type MCPTool struct {
	Name         string // Prefixed name: "github.list_repos"
	ServerName   string // Server that provides this tool
	OriginalName string // Original name without prefix
	Description  string
	InputSchema  map[string]interface{}
}

// MCPConnection represents a connection to an MCP server
type MCPConnection struct {
	Config      MCPServerConfig
	Client      *Client
	Tools       []MCPTool
	Status      ConnectionStatus
	ConnectedAt time.Time
	Error       error
}

// ConnectionStatus represents the status of an MCP server connection
type ConnectionStatus string

const (
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
	StatusError        ConnectionStatus = "error"
)

// String returns the string representation of ConnectionStatus
func (s ConnectionStatus) String() string {
	return string(s)
}
