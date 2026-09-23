// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"context"
	"fmt"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/mcp"
)

// cmdMCP is the /mcp command: MCP servers and their tools.
func (r *repl) cmdMCP(args []string) {
	handleMCP(r.mcp, args, r.asst)
}

// handleMCP handles the /mcp command for managing MCP server connections
func handleMCP(mgr *mcp.Manager, args []string, asst *assistant.Assistant) {
	if mgr == nil {
		printMCPNotEnabled()
		return
	}
	if len(args) == 0 {
		printMCPServers(mgr)
		return
	}

	switch args[0] {
	case "connect":
		mcpConnect(mgr, args, asst)
	case "disconnect":
		mcpDisconnect(mgr, args, asst)
	case "tools":
		printMCPTools(mgr)
	default:
		fmt.Printf("Unknown subcommand: %s\n", args[0])
		fmt.Println("Usage: /mcp [connect|disconnect|tools]")
		fmt.Println()
	}
}

// printMCPNotEnabled explains how to turn MCP on when no manager is configured.
func printMCPNotEnabled() {
	fmt.Println("MCP is not enabled. Add MCP servers to your config file.")
	fmt.Println()
	fmt.Println("Example ~/.taracode/config.yaml:")
	fmt.Println("  mcp:")
	fmt.Println("    enabled: true")
	fmt.Println("    servers:")
	fmt.Println("      - name: github")
	fmt.Println("        command: npx")
	fmt.Println("        args: [\"-y\", \"@modelcontextprotocol/server-github\"]")
	fmt.Println("        env:")
	fmt.Println("          GITHUB_TOKEN: \"${GITHUB_TOKEN}\"")
	fmt.Println()
}

// printMCPServers shows the configured servers and their live connection status.
func printMCPServers(mgr *mcp.Manager) {
	servers := mgr.GetConfiguredServers()
	connections := mgr.GetAllConnections()

	if len(servers) == 0 {
		fmt.Println("No MCP servers configured.")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Println("MCP Servers:")
	fmt.Println()

	for _, srv := range servers {
		status := "disconnected"
		toolCount := 0

		if conn, ok := connections[srv.Name]; ok {
			status = string(conn.Status)
			toolCount = len(conn.Tools)
		}

		autoConnect := ""
		if srv.AutoConnect {
			autoConnect = " (auto-connect)"
		}

		fmt.Printf("  %-15s %s%s", srv.Name, formatMCPStatus(status), autoConnect)
		if toolCount > 0 {
			fmt.Printf("  [%d tools]", toolCount)
		}
		fmt.Println()
	}

	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  /mcp connect <name>    - Connect to a server")
	fmt.Println("  /mcp disconnect <name> - Disconnect from a server")
	fmt.Println("  /mcp tools             - List tools from connected servers")
	fmt.Println()
}

// formatMCPStatus color-codes a server's connection status for the /mcp listing.
func formatMCPStatus(status string) string {
	switch status {
	case "connected":
		return fmt.Sprintf("\033[32m%s\033[0m", status)
	case "connecting":
		return fmt.Sprintf("\033[33m%s\033[0m", status)
	case "error":
		return fmt.Sprintf("\033[31m%s\033[0m", status)
	default:
		return fmt.Sprintf("\033[90m%s\033[0m", status)
	}
}

// mcpConnect handles "/mcp connect <server-name>".
func mcpConnect(mgr *mcp.Manager, args []string, asst *assistant.Assistant) {
	if len(args) < 2 {
		fmt.Println("Usage: /mcp connect <server-name>")
		fmt.Println()
		return
	}
	serverName := args[1]
	fmt.Printf("Connecting to %s...\n", serverName)

	if err := mgr.Connect(context.Background(), serverName); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println()
		return
	}

	// Get tool count
	mcpTools := mgr.GetToolsByServer(serverName)
	fmt.Printf("Connected to %s. Discovered %d tools.\n", serverName, len(mcpTools))

	// Register the tools with the assistant's registry (re-registering a name replaces it, so the
	// discovery callback having done the same is harmless) and refresh the exposed schemas
	registry := asst.ToolRegistry()
	for _, tool := range mcpTools {
		registry.RegisterMCP(mcp.ToTool(mgr, tool), tool.ServerName)
	}
	asst.RefreshTools()
	fmt.Println()
}

// mcpDisconnect handles "/mcp disconnect <server-name>".
func mcpDisconnect(mgr *mcp.Manager, args []string, asst *assistant.Assistant) {
	if len(args) < 2 {
		fmt.Println("Usage: /mcp disconnect <server-name>")
		fmt.Println()
		return
	}
	serverName := args[1]

	// Unregister the server's tools and refresh the exposed schemas
	asst.ToolRegistry().UnregisterMCP(serverName)
	asst.RefreshTools()

	if err := mgr.Disconnect(serverName); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Disconnected from %s.\n", serverName)
	fmt.Println()
}

// printMCPTools handles "/mcp tools": every tool from every connected server, grouped by server.
func printMCPTools(mgr *mcp.Manager) {
	mcpTools := mgr.GetAllTools()
	if len(mcpTools) == 0 {
		fmt.Println("No MCP tools available. Connect to a server first.")
		fmt.Println("  /mcp connect <name>")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("MCP Tools (%d total):\n", len(mcpTools))
	fmt.Println()

	// Group tools by server
	toolsByServer := make(map[string][]mcp.MCPTool)
	for _, tool := range mcpTools {
		toolsByServer[tool.ServerName] = append(toolsByServer[tool.ServerName], tool)
	}

	for serverName, serverTools := range toolsByServer {
		fmt.Printf("  %s:\n", serverName)
		for _, t := range serverTools {
			desc := t.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			fmt.Printf("    %-30s %s\n", t.Name, desc)
		}
		fmt.Println()
	}
}
