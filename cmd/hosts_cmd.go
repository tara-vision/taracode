package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tara-vision/taracode/internal/orchestrator"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/ui"
)

// Host command specific styles
var (
	hostHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(ui.Primary)
	hostBoldStyle   = lipgloss.NewStyle().Bold(true)
	hostDimStyle    = lipgloss.NewStyle().Foreground(ui.Muted)
	hostInfoStyle   = lipgloss.NewStyle().Foreground(ui.Info)
)

// handleHostsCommand handles the /hosts command for multi-host management
func handleHostsCommand(args []string, taskBridge *orchestrator.TaskBridge) {
	if taskBridge == nil || !taskBridge.HasHostPool() {
		// Check if multi-host is configured
		hostsCfg := GetHostsConfig()
		if hostsCfg.IsEmpty() || len(hostsCfg.Hosts) <= 1 {
			fmt.Println("Multi-host not configured.")
			fmt.Println()
			fmt.Println("To use multiple hosts, add to ~/.taracode/config.yaml:")
			fmt.Println()
			fmt.Println("  hosts:")
			fmt.Println("    primary:")
			fmt.Println("      url: http://gpu-server:11434")
			fmt.Println("      priority: 1")
			fmt.Println("    local:")
			fmt.Println("      url: http://localhost:11434")
			fmt.Println("      fallback: primary")
			fmt.Println("      priority: 2")
			fmt.Println("  default_host: primary")
			fmt.Println()
			fmt.Println("Currently using single host mode.")
			fmt.Println()
			return
		}
		fmt.Println("Host pool not initialized. Run /init first.")
		fmt.Println()
		return
	}

	hostPool := taskBridge.GetHostPool()

	// Handle subcommands
	if len(args) > 0 {
		switch args[0] {
		case "check":
			handleHostsCheck(hostPool)
			return
		case "reconnect":
			handleHostsReconnect(hostPool)
			return
		case "help":
			handleHostsHelp()
			return
		default:
			fmt.Printf("Unknown hosts subcommand: %s\n", args[0])
			handleHostsHelp()
			return
		}
	}

	// Default: show host status
	handleHostsStatus(hostPool)
}

// handleHostsStatus displays the status of all configured hosts
func handleHostsStatus(hostPool *provider.HostPool) {
	infos := hostPool.GetHostInfo()

	if len(infos) == 0 {
		fmt.Println("No hosts configured.")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("  %s  Hosts (%d/%d healthy)\n",
		hostHeaderStyle.Render("HOST STATUS"),
		hostPool.HealthyCount(),
		hostPool.HostCount())
	fmt.Println()

	// Display each host
	for _, info := range infos {
		// Status indicator
		statusIcon := getStatusIcon(info.Status)
		statusColor := getStatusColor(info.Status)

		// Default marker
		defaultMarker := ""
		if info.IsDefault {
			defaultMarker = " [default]"
		}

		// Format name with status
		fmt.Printf("  %s %s%s\n",
			statusColor.Render(statusIcon),
			hostBoldStyle.Render(info.Name),
			hostDimStyle.Render(defaultMarker))

		// Host details
		fmt.Printf("      URL:      %s\n", info.URL)
		if info.Vendor != "" {
			fmt.Printf("      Vendor:   %s\n", info.Vendor)
		}
		fmt.Printf("      Status:   %s\n", statusColor.Render(string(info.Status)))

		if info.Status == provider.HostStatusHealthy {
			fmt.Printf("      Latency:  %v\n", info.Latency.Round(time.Millisecond))
			if len(info.Models) > 0 {
				modelList := strings.Join(info.Models, ", ")
				if len(modelList) > 50 {
					modelList = modelList[:47] + "..."
				}
				fmt.Printf("      Models:   %d (%s)\n", len(info.Models), modelList)
			}
		} else if info.LastError != "" {
			fmt.Printf("      Error:    %s\n", ui.ErrorStyle.Render(info.LastError))
		}

		if info.Fallback != "" {
			fmt.Printf("      Fallback: %s\n", info.Fallback)
		}

		if !info.LastChecked.IsZero() {
			ago := time.Since(info.LastChecked).Round(time.Second)
			fmt.Printf("      Checked:  %v ago\n", ago)
		}

		fmt.Println()
	}

	fmt.Println("  Use /hosts check to force health check on all hosts")
	fmt.Println()
}

// handleHostsCheck forces a health check on all hosts
func handleHostsCheck(hostPool *provider.HostPool) {
	infos := hostPool.GetHostInfo()

	fmt.Println()
	fmt.Println("  Checking host health...")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, info := range infos {
		fmt.Printf("  Checking %s... ", info.Name)

		start := time.Now()
		healthy := hostPool.CheckHealth(ctx, info.Name)
		duration := time.Since(start)

		if healthy {
			fmt.Printf("%s (%v)\n",
				ui.SuccessStyle.Render("healthy"),
				duration.Round(time.Millisecond))
		} else {
			conn, _ := hostPool.GetConnection(info.Name)
			errMsg := "unknown error"
			if conn != nil && conn.LastError != nil {
				errMsg = conn.LastError.Error()
			}
			fmt.Printf("%s (%s)\n",
				ui.ErrorStyle.Render("unhealthy"),
				errMsg)
		}
	}

	fmt.Println()
	fmt.Printf("  Summary: %d/%d hosts healthy\n",
		hostPool.HealthyCount(),
		hostPool.HostCount())
	fmt.Println()
}

// handleHostsReconnect attempts to reconnect to unhealthy hosts
func handleHostsReconnect(hostPool *provider.HostPool) {
	fmt.Println()
	fmt.Println("  Reconnecting to unhealthy hosts...")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := hostPool.Reconnect(ctx)
	if err != nil {
		fmt.Printf("  %s Some hosts failed to reconnect: %v\n",
			ui.WarningStyle.Render(ui.IconWarning), err)
	}

	fmt.Printf("  Summary: %d/%d hosts healthy\n",
		hostPool.HealthyCount(),
		hostPool.HostCount())
	fmt.Println()
}

// handleHostsHelp shows help for hosts commands
func handleHostsHelp() {
	fmt.Println()
	fmt.Println("  HOST COMMANDS")
	fmt.Println()
	fmt.Println("  /hosts              Show status of all configured hosts")
	fmt.Println("  /hosts check        Force health check on all hosts")
	fmt.Println("  /hosts reconnect    Attempt to reconnect unhealthy hosts")
	fmt.Println("  /hosts help         Show this help message")
	fmt.Println()
	fmt.Println("  CONFIGURATION")
	fmt.Println()
	fmt.Println("  Add to ~/.taracode/config.yaml:")
	fmt.Println()
	fmt.Println("    hosts:")
	fmt.Println("      primary:")
	fmt.Println("        url: http://gpu-server:11434")
	fmt.Println("        models: [gemma3:27b, qwen2.5-coder:32b]")
	fmt.Println("        priority: 1")
	fmt.Println("      local:")
	fmt.Println("        url: http://localhost:11434")
	fmt.Println("        fallback: primary")
	fmt.Println("        priority: 2")
	fmt.Println("    default_host: primary")
	fmt.Println()
	fmt.Println("  Per-agent host assignment in agents config:")
	fmt.Println()
	fmt.Println("    agents:")
	fmt.Println("      coder:")
	fmt.Println("        host: primary")
	fmt.Println("        model: qwen2.5-coder:32b")
	fmt.Println("      reviewer:")
	fmt.Println("        host: local")
	fmt.Println("        model: llama3.2:3b")
	fmt.Println()
}

// getStatusIcon returns an icon for the host status
func getStatusIcon(status provider.HostStatus) string {
	switch status {
	case provider.HostStatusHealthy:
		return ui.IconSuccess
	case provider.HostStatusConnecting:
		return ui.IconThinking
	case provider.HostStatusUnhealthy:
		return ui.IconWarning
	case provider.HostStatusUnavailable:
		return ui.IconError
	default:
		return ui.IconInfo
	}
}

// getStatusColor returns a style for the host status
func getStatusColor(status provider.HostStatus) lipgloss.Style {
	switch status {
	case provider.HostStatusHealthy:
		return ui.SuccessStyle
	case provider.HostStatusConnecting:
		return hostInfoStyle
	case provider.HostStatusUnhealthy:
		return ui.WarningStyle
	case provider.HostStatusUnavailable:
		return ui.ErrorStyle
	default:
		return hostDimStyle
	}
}
