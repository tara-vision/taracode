// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// cmdMode is the /mode command: show or switch the operating mode.
func (r *repl) cmdMode(args []string) {
	if len(args) == 0 {
		// Show current mode and available modes
		currentMode := r.asst.GetMode()
		if currentMode == storage.ModeSecurity {
			fmt.Printf("Current mode: %s %s\n", ui.IconShield, currentMode)
		} else {
			fmt.Printf("Current mode: %s\n", currentMode)
		}
		fmt.Println("Available modes: devops, security")
	} else {
		newMode := args[0]
		previousMode := r.asst.GetMode()
		if err := r.asst.SetMode(newMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		// Show mode activation message
		modeRenderer := ui.NewRenderer()
		targetMode := storage.OperatingMode(newMode)
		if targetMode == storage.ModeSecurity && previousMode != storage.ModeSecurity {
			fmt.Print(modeRenderer.SecurityModeActivatedMessage())
		} else if targetMode == storage.ModeDevOps && previousMode == storage.ModeSecurity {
			fmt.Print(modeRenderer.SecurityModeDeactivatedMessage())
		} else {
			fmt.Printf("Switched to %s mode.\n", newMode)
		}
	}
	fmt.Println()
}

// cmdPermissions is the /permissions command: tool permission settings.
func (r *repl) cmdPermissions(args []string) {
	handlePermissions(r.asst, args)
}

// cmdAudit is the /audit command: the security audit log.
func (r *repl) cmdAudit(args []string) {
	handleAudit(r.asst, args)
}

// handleAudit handles the /audit command for viewing and exporting security audit logs
func handleAudit(asst *assistant.Assistant, args []string) {
	// Check if in security mode
	if asst.GetMode() != storage.ModeSecurity {
		fmt.Println("Audit log is only available in security mode.")
		fmt.Println("Switch to security mode with: /mode security")
		fmt.Println()
		return
	}

	session := asst.GetSessionFresh()
	if session == nil {
		fmt.Println("No active session.")
		fmt.Println()
		return
	}

	auditLog := session.AuditLog
	hasEntries := auditLog != nil && len(auditLog.Entries) > 0

	// Handle subcommands first (some work even without entries)
	if len(args) > 0 {
		switch args[0] {
		case "export":
			if len(args) < 2 {
				fmt.Println("Usage: /audit export <json|html>")
				fmt.Println()
				return
			}
			format := args[1]
			if format != "json" && format != "html" {
				fmt.Printf("Unknown export format: %s\n", format)
				fmt.Println("Supported formats: json, html")
				fmt.Println()
				return
			}
			// Check for entries before export
			if !hasEntries {
				fmt.Println("No audit entries to export.")
				fmt.Println("Audit entries are created when you allow or deny tool operations.")
				fmt.Println()
				return
			}
			if format == "json" {
				exportAuditJSON(session, auditLog)
			} else {
				exportAuditHTML(session, auditLog)
			}
		case "clear":
			if !hasEntries {
				fmt.Println("Audit log is already empty.")
				fmt.Println()
				return
			}
			if err := asst.ClearAuditLog(); err != nil {
				fmt.Fprintf(os.Stderr, "Error clearing audit log: %v\n", err)
				return
			}
			fmt.Println("Audit log cleared.")
			fmt.Println()
		default:
			fmt.Printf("Unknown audit subcommand: %s\n", args[0])
			fmt.Println("Usage: /audit [export <json|html>|clear]")
			fmt.Println()
		}
		return
	}

	// No args - display audit log
	if !hasEntries {
		fmt.Println("No audit entries recorded in this session.")
		fmt.Println("Audit entries are created when you allow or deny tool operations.")
		fmt.Println()
		return
	}

	displayAuditLog(auditLog)
}

// displayAuditLog shows the audit log in a formatted table
func displayAuditLog(log *storage.AuditLog) {
	fmt.Println()
	fmt.Printf("%s Security Audit Log\n", ui.IconShield)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Total entries: %d (Allowed: %d, Denied: %d)\n", len(log.Entries), log.TotalAllow, log.TotalDeny)
	fmt.Println()

	// Display entries in reverse chronological order (most recent first)
	for i := len(log.Entries) - 1; i >= 0; i-- {
		entry := log.Entries[i]
		timestamp := entry.Timestamp.Format("15:04:05")

		// Action indicator with color
		var actionSymbol string
		switch entry.Action {
		case storage.AuditActionAllow:
			actionSymbol = fmt.Sprintf("\033[32m%s ALLOW\033[0m", ui.IconSuccess)
		case storage.AuditActionAllowAll:
			actionSymbol = fmt.Sprintf("\033[32m%s ALLOW ALL\033[0m", ui.IconSuccess)
		case storage.AuditActionDeny:
			actionSymbol = fmt.Sprintf("\033[31m%s DENY\033[0m", ui.IconError)
		case storage.AuditActionDenyAll:
			actionSymbol = fmt.Sprintf("\033[31m%s DENY ALL\033[0m", ui.IconError)
		default:
			actionSymbol = string(entry.Action)
		}

		// Category with color
		var categoryColor string
		switch entry.Category {
		case "destructive":
			categoryColor = "\033[31m" // Red
		case "execute":
			categoryColor = "\033[33m" // Yellow
		case "write", "git":
			categoryColor = "\033[36m" // Cyan
		default:
			categoryColor = "\033[0m"
		}

		fmt.Printf("[%s] %s %s%s\033[0m: %s\n", timestamp, actionSymbol, categoryColor, entry.Category, entry.ToolName)
		if entry.Target != "" {
			targetDisplay := entry.Target
			if len(targetDisplay) > 50 {
				targetDisplay = targetDisplay[:47] + "..."
			}
			fmt.Printf("         └─ %s\n", targetDisplay)
		}
	}
	fmt.Println()
}

// exportAuditJSON exports the audit log to a JSON file
func exportAuditJSON(session *storage.Session, log *storage.AuditLog) {
	// Create export structure
	export := struct {
		SessionID   string `json:"session_id"`
		SessionName string `json:"session_name"`
		ExportedAt  string `json:"exported_at"`
		Mode        string `json:"mode"`
		Summary     struct {
			TotalEntries int `json:"total_entries"`
			TotalAllow   int `json:"total_allow"`
			TotalDeny    int `json:"total_deny"`
		} `json:"summary"`
		Entries []storage.AuditEntry `json:"entries"`
	}{
		SessionID:   session.ID,
		SessionName: session.Name,
		ExportedAt:  time.Now().Format(time.RFC3339),
		Mode:        log.SessionMode,
		Entries:     log.Entries,
	}
	export.Summary.TotalEntries = len(log.Entries)
	export.Summary.TotalAllow = log.TotalAllow
	export.Summary.TotalDeny = log.TotalDeny

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling audit log: %v\n", err)
		return
	}

	// Generate filename
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("audit-%s-%s.json", ui.TruncateID(session.ID, 8), timestamp)

	//nolint:gosec // /audit export writes a generated report filename into the current directory
	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		return
	}

	fmt.Printf("%s Audit log exported to: %s\n", ui.IconSuccess, filename)
	fmt.Println()
}

// exportAuditHTML exports the audit log to an HTML report
func exportAuditHTML(session *storage.Session, log *storage.AuditLog) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("audit-%s-%s.html", ui.TruncateID(session.ID, 8), timestamp)

	var html strings.Builder

	// HTML header with embedded styles
	html.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Security Audit Report</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            max-width: 900px;
            margin: 0 auto;
            padding: 20px;
            background: #1a1a2e;
            color: #eee;
        }
        h1 {
            color: #f97316;
            border-bottom: 2px solid #f97316;
            padding-bottom: 10px;
        }
        .summary {
            background: #16213e;
            padding: 15px;
            border-radius: 8px;
            margin-bottom: 20px;
        }
        .summary-stats {
            display: flex;
            gap: 20px;
            margin-top: 10px;
        }
        .stat {
            padding: 10px 20px;
            border-radius: 4px;
            font-weight: bold;
        }
        .stat-allow { background: #065f46; }
        .stat-deny { background: #991b1b; }
        .stat-total { background: #1e40af; }
        table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 20px;
        }
        th, td {
            padding: 12px;
            text-align: left;
            border-bottom: 1px solid #333;
        }
        th {
            background: #16213e;
            color: #f97316;
        }
        tr:hover {
            background: #16213e;
        }
        .action-allow { color: #10b981; font-weight: bold; }
        .action-deny { color: #ef4444; font-weight: bold; }
        .category-destructive { color: #ef4444; }
        .category-execute { color: #f59e0b; }
        .category-write, .category-git { color: #06b6d4; }
        .target {
            font-family: monospace;
            font-size: 0.9em;
            color: #9ca3af;
            max-width: 300px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        .footer {
            margin-top: 30px;
            padding-top: 20px;
            border-top: 1px solid #333;
            color: #6b7280;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <h1>🛡️ Security Audit Report</h1>
`)

	// Summary section
	fmt.Fprintf(&html, `    <div class="summary">
        <strong>Session:</strong> %s<br>
        <strong>Session ID:</strong> %s<br>
        <strong>Mode:</strong> %s<br>
        <strong>Generated:</strong> %s
        <div class="summary-stats">
            <span class="stat stat-total">Total: %d</span>
            <span class="stat stat-allow">Allowed: %d</span>
            <span class="stat stat-deny">Denied: %d</span>
        </div>
    </div>
`,
		escapeHTML(session.Name),
		session.ID,
		log.SessionMode,
		time.Now().Format("2006-01-02 15:04:05"),
		len(log.Entries),
		log.TotalAllow,
		log.TotalDeny,
	)

	// Entries table
	html.WriteString(`    <table>
        <thead>
            <tr>
                <th>Time</th>
                <th>Action</th>
                <th>Category</th>
                <th>Tool</th>
                <th>Target</th>
            </tr>
        </thead>
        <tbody>
`)

	for _, entry := range log.Entries {
		actionClass := "action-allow"
		if entry.Action == storage.AuditActionDeny || entry.Action == storage.AuditActionDenyAll {
			actionClass = "action-deny"
		}

		categoryClass := fmt.Sprintf("category-%s", entry.Category)

		target := entry.Target
		if len(target) > 50 {
			target = target[:47] + "..."
		}

		fmt.Fprintf(&html, `            <tr>
                <td>%s</td>
                <td class="%s">%s</td>
                <td class="%s">%s</td>
                <td>%s</td>
                <td class="target" title="%s">%s</td>
            </tr>
`,
			entry.Timestamp.Format("15:04:05"),
			actionClass,
			escapeHTML(string(entry.Action)),
			categoryClass,
			escapeHTML(entry.Category),
			escapeHTML(entry.ToolName),
			escapeHTML(entry.Target),
			escapeHTML(target),
		)
	}

	html.WriteString(`        </tbody>
    </table>
    <div class="footer">
        Generated by Tara Code Security Mode
    </div>
</body>
</html>
`)

	//nolint:gosec // /audit export writes a generated report filename into the current directory
	if err := os.WriteFile(filename, []byte(html.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		return
	}

	fmt.Printf("%s Audit report exported to: %s\n", ui.IconSuccess, filename)
	fmt.Println()
}

// escapeHTML escapes special HTML characters
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// handlePermissions handles the /permissions command
func handlePermissions(asst *assistant.Assistant, args []string) {
	permMgr := asst.GetPermissionManager()
	if permMgr == nil {
		fmt.Println("Permission manager not initialized. Run /init first.")
		fmt.Println()
		return
	}

	if len(args) == 0 {
		// Show current permissions
		config := permMgr.GetConfig()

		fmt.Println("Permission Settings:")
		fmt.Println()

		// Show category settings
		fmt.Println("  Categories:")
		for _, cat := range permissions.GetAllCategories() {
			perm := permissions.GetDefaultPermission(cat)
			if customPerm, ok := config.Categories[cat]; ok {
				perm = customPerm
			}
			desc := permissions.GetCategoryDescription(cat)
			fmt.Printf("    %-12s → %-5s  %s\n", cat, perm, desc)
		}
		fmt.Println()

		// Show tool-specific settings
		if len(config.Tools) > 0 {
			fmt.Println("  Tool Overrides:")
			for tool, perm := range config.Tools {
				fmt.Printf("    %-20s → %s\n", tool, perm)
			}
			fmt.Println()
		}

		fmt.Println("  Commands:")
		fmt.Println("    /permissions reset              - Reset all to default")
		fmt.Println("    /permissions allow <tool|cat>   - Always allow")
		fmt.Println("    /permissions deny <tool|cat>    - Always deny")
		fmt.Println("    /permissions ask <tool|cat>     - Always ask")
		fmt.Println()
		return
	}

	subCmd := args[0]

	switch subCmd {
	case "reset":
		if err := permMgr.Reset(); err != nil {
			fmt.Fprintf(os.Stderr, "Error resetting permissions: %v\n", err)
			return
		}
		fmt.Println("Permissions reset to default.")
		fmt.Println()

	case "allow", "deny", "ask":
		if len(args) < 2 {
			fmt.Printf("Usage: /permissions %s <tool|category>\n", subCmd)
			fmt.Println()
			return
		}

		target := args[1]
		var perm permissions.Permission
		switch subCmd {
		case "allow":
			perm = permissions.PermissionAllow
		case "deny":
			perm = permissions.PermissionDeny
		case "ask":
			perm = permissions.PermissionAsk
		}

		// Check if it's a category
		if permissions.IsValidCategory(target) {
			cat := permissions.PermissionCategory(target)
			if err := permMgr.SetCategoryPermission(cat, perm); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting permission: %v\n", err)
				return
			}
			fmt.Printf("Category '%s' → %s\n", target, perm)
		} else if permissions.IsValidTool(target) {
			if err := permMgr.SetToolPermission(target, perm); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting permission: %v\n", err)
				return
			}
			fmt.Printf("Tool '%s' → %s\n", target, perm)
		} else {
			fmt.Printf("Unknown tool or category: %s\n", target)
			fmt.Println("Valid categories: read, write, execute, git, destructive, mcp")
			fmt.Println("Use /tools to see valid tool names.")
		}
		fmt.Println()

	default:
		fmt.Printf("Unknown subcommand: %s\n", subCmd)
		fmt.Println("Usage: /permissions [reset|allow|deny|ask] [target]")
		fmt.Println()
	}
}
