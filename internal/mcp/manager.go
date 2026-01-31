package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ToolDiscoveryCallback is called when tools are discovered from an MCP server
type ToolDiscoveryCallback func(serverName string, tools []MCPTool)

// Manager manages multiple MCP server connections
type Manager struct {
	config      MCPConfig
	connections map[string]*MCPConnection
	mu          sync.RWMutex
	callback    ToolDiscoveryCallback
}

// NewManager creates a new MCP manager
func NewManager(config MCPConfig) *Manager {
	return &Manager{
		config:      config,
		connections: make(map[string]*MCPConnection),
	}
}

// SetToolDiscoveryCallback sets a callback to be called when tools are discovered
func (m *Manager) SetToolDiscoveryCallback(callback ToolDiscoveryCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callback = callback
}

// AutoConnect connects to all servers with auto_connect: true
func (m *Manager) AutoConnect(ctx context.Context) {
	for _, serverCfg := range m.config.Servers {
		if serverCfg.AutoConnect {
			if err := m.Connect(ctx, serverCfg.Name); err != nil {
				// Log warning but don't fail
				fmt.Printf("Warning: Failed to auto-connect to MCP server '%s': %v\n", serverCfg.Name, err)
			}
		}
	}
}

// Connect connects to an MCP server by name
func (m *Manager) Connect(ctx context.Context, name string) error {
	// Find server config
	var serverCfg *MCPServerConfig
	for i := range m.config.Servers {
		if m.config.Servers[i].Name == name {
			serverCfg = &m.config.Servers[i]
			break
		}
	}

	if serverCfg == nil {
		return fmt.Errorf("unknown MCP server: %s", name)
	}

	m.mu.Lock()
	// Check if already connected
	if conn, exists := m.connections[name]; exists && conn.Status == StatusConnected {
		m.mu.Unlock()
		return fmt.Errorf("already connected to %s", name)
	}

	// Create connection entry
	conn := &MCPConnection{
		Config: *serverCfg,
		Status: StatusConnecting,
	}
	m.connections[name] = conn
	m.mu.Unlock()

	// Set timeout
	timeout := serverCfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create client
	client, err := NewClient(serverCfg.Command, serverCfg.Args, serverCfg.Env, timeout)
	if err != nil {
		m.mu.Lock()
		conn.Status = StatusError
		conn.Error = err
		m.mu.Unlock()
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	// Connect (initialize handshake)
	if err := client.Connect(connectCtx); err != nil {
		client.Close()
		m.mu.Lock()
		conn.Status = StatusError
		conn.Error = err
		m.mu.Unlock()
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Discover tools
	toolInfos, err := client.ListTools(connectCtx)
	if err != nil {
		client.Close()
		m.mu.Lock()
		conn.Status = StatusError
		conn.Error = err
		m.mu.Unlock()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Convert to MCPTool with prefixed names
	tools := make([]MCPTool, len(toolInfos))
	for i, ti := range toolInfos {
		tools[i] = MCPTool{
			Name:         fmt.Sprintf("%s.%s", name, ti.Name),
			ServerName:   name,
			OriginalName: ti.Name,
			Description:  ti.Description,
			InputSchema:  ti.InputSchema,
		}
	}

	// Update connection
	m.mu.Lock()
	conn.Client = client
	conn.Tools = tools
	conn.Status = StatusConnected
	conn.ConnectedAt = time.Now()
	conn.Error = nil
	callback := m.callback
	m.mu.Unlock()

	// Notify callback
	if callback != nil {
		callback(name, tools)
	}

	return nil
}

// Disconnect disconnects from an MCP server
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	conn, exists := m.connections[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("not connected to %s", name)
	}

	if conn.Status != StatusConnected || conn.Client == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to %s", name)
	}

	client := conn.Client
	conn.Status = StatusDisconnected
	conn.Client = nil
	conn.Tools = nil
	m.mu.Unlock()

	// Close the client
	return client.Close()
}

// GetConnection returns the connection for a server
func (m *Manager) GetConnection(name string) *MCPConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[name]
}

// GetAllConnections returns all connections
func (m *Manager) GetAllConnections() map[string]*MCPConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*MCPConnection)
	for name, conn := range m.connections {
		result[name] = conn
	}
	return result
}

// GetConfiguredServers returns all configured server names
func (m *Manager) GetConfiguredServers() []MCPServerConfig {
	return m.config.Servers
}

// GetAllTools returns all tools from all connected servers
func (m *Manager) GetAllTools() []MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []MCPTool
	for _, conn := range m.connections {
		if conn.Status == StatusConnected {
			tools = append(tools, conn.Tools...)
		}
	}
	return tools
}

// GetToolsByServer returns tools for a specific server
func (m *Manager) GetToolsByServer(name string) []MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if conn, exists := m.connections[name]; exists && conn.Status == StatusConnected {
		return conn.Tools
	}
	return nil
}

// CallTool calls a tool by its prefixed name (e.g., "github.list_repos")
func (m *Manager) CallTool(ctx context.Context, name string, params map[string]interface{}) (string, error) {
	m.mu.RLock()

	// Find the tool and its server
	var conn *MCPConnection
	var tool *MCPTool
	for _, c := range m.connections {
		if c.Status != StatusConnected {
			continue
		}
		for i := range c.Tools {
			if c.Tools[i].Name == name {
				conn = c
				tool = &c.Tools[i]
				break
			}
		}
		if tool != nil {
			break
		}
	}
	m.mu.RUnlock()

	if conn == nil || tool == nil {
		return "", fmt.Errorf("unknown MCP tool: %s", name)
	}

	// Call the tool with original name
	result, err := conn.Client.CallTool(ctx, tool.OriginalName, params)
	if err != nil {
		return "", err
	}

	// Format result
	if result.IsError {
		var errMsg string
		for _, item := range result.Content {
			if item.Type == "text" {
				errMsg += item.Text
			}
		}
		return "", fmt.Errorf("tool error: %s", errMsg)
	}

	var output string
	for _, item := range result.Content {
		if item.Type == "text" {
			output += item.Text
		}
	}

	return output, nil
}

// IsConnected returns whether a server is connected
func (m *Manager) IsConnected(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if conn, exists := m.connections[name]; exists {
		return conn.Status == StatusConnected
	}
	return false
}

// Close closes all connections
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, conn := range m.connections {
		if conn.Client != nil {
			conn.Client.Close()
			conn.Client = nil
		}
		conn.Status = StatusDisconnected
		conn.Tools = nil
		delete(m.connections, name)
	}
}
