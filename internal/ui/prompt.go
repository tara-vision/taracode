package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/manifoldco/promptui"
	"github.com/tara-vision/taracode/internal/permissions"
)

// PermissionChoice represents a user's permission choice
type PermissionChoice struct {
	Allowed    bool                         // Whether this execution is allowed
	SavePerm   permissions.Permission       // Permission to save (empty if not saving)
	SaveScope  string                       // "tool" or "category" (empty if not saving)
	Category   permissions.PermissionCategory // The category (for category saves)
}

// PromptToolPermission asks the user for permission to execute a tool
// Returns the user's choice
func PromptToolPermission(toolName string, params map[string]interface{}) PermissionChoice {
	category := permissions.GetToolCategory(toolName)

	// Build params summary
	paramsSummary := formatParams(params)

	// Display the tool info
	fmt.Println()
	fmt.Printf("%s Tool: %s\n", IconWarning, Bold.Render(toolName))
	if paramsSummary != "" {
		fmt.Printf("   Params: %s\n", Subtle.Render(paramsSummary))
	}
	fmt.Printf("   Category: %s\n", Subtle.Render(string(category)))
	fmt.Println()

	// Define choices
	type choice struct {
		Label string
		Short string
	}

	choices := []choice{
		{Label: "Yes, allow this time", Short: "y"},
		{Label: "No, deny this time", Short: "n"},
		{Label: fmt.Sprintf("Always allow %s", toolName), Short: "a"},
		{Label: fmt.Sprintf("Always deny %s", toolName), Short: "d"},
		{Label: fmt.Sprintf("Always allow %s category", category), Short: "c"},
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "\U0001F449 {{ .Label | cyan }}",
		Inactive: "   {{ .Label }}",
		Selected: "\U00002705 {{ .Label | green }}",
	}

	prompt := promptui.Select{
		Label:     "Allow this tool execution?",
		Items:     choices,
		Templates: templates,
		Size:      5,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		// User cancelled - treat as deny
		return PermissionChoice{Allowed: false}
	}

	switch idx {
	case 0: // Yes, allow this time
		return PermissionChoice{Allowed: true}
	case 1: // No, deny this time
		return PermissionChoice{Allowed: false}
	case 2: // Always allow tool
		return PermissionChoice{
			Allowed:   true,
			SavePerm:  permissions.PermissionAllow,
			SaveScope: "tool",
		}
	case 3: // Always deny tool
		return PermissionChoice{
			Allowed:   false,
			SavePerm:  permissions.PermissionDeny,
			SaveScope: "tool",
		}
	case 4: // Always allow category
		return PermissionChoice{
			Allowed:   true,
			SavePerm:  permissions.PermissionAllow,
			SaveScope: "category",
			Category:  category,
		}
	}

	return PermissionChoice{Allowed: false}
}

// formatParams creates a concise summary of tool parameters
func formatParams(params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}

	var parts []string
	for k, v := range params {
		// Skip long values
		str := fmt.Sprintf("%v", v)
		if len(str) > 50 {
			str = str[:47] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, str))
	}

	result := strings.Join(parts, ", ")
	if len(result) > 80 {
		result = result[:77] + "..."
	}
	return result
}

// ConfirmAction shows a simple yes/no confirmation prompt
func ConfirmAction(message string) bool {
	prompt := promptui.Prompt{
		Label:     message,
		IsConfirm: true,
	}

	result, err := prompt.Run()
	if err != nil {
		return false
	}

	return strings.ToLower(result) == "y"
}

// DisplayPermissionDenied shows a message when a tool is blocked
func DisplayPermissionDenied(toolName string) {
	fmt.Println(ToolError.Render(fmt.Sprintf("%s Tool '%s' blocked by permission settings", IconError, toolName)))
}

// DisplayPermissionSaved shows confirmation when a permission is saved
func DisplayPermissionSaved(scope, target string, perm permissions.Permission) {
	var permWord string
	switch perm {
	case permissions.PermissionAllow:
		permWord = "allow"
	case permissions.PermissionDeny:
		permWord = "deny"
	default:
		permWord = "ask"
	}
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("%s Saved: %s %s → %s", IconSuccess, scope, target, permWord)))
}

// SecurityAuditChoice represents the user's choice in a security audit prompt
type SecurityAuditChoice int

const (
	SecurityAuditAllow SecurityAuditChoice = iota
	SecurityAuditDeny
	SecurityAuditDetails
	SecurityAuditAllowAll // Allow all remaining operations in batch
	SecurityAuditDenyAll  // Deny all remaining operations in batch
)

// SecurityAuditInfo contains information about a security-sensitive operation
type SecurityAuditInfo struct {
	ToolName    string
	Category    permissions.PermissionCategory
	Params      map[string]interface{}
	Implication string // Security implication description
}

// BatchAuditContext tracks batch confirmation state for multi-tool responses
type BatchAuditContext struct {
	TotalTools     int  // Total number of tools requiring audit
	CurrentIndex   int  // Current tool index (1-based for display)
	AllowRemaining bool // User chose to allow all remaining
	DenyRemaining  bool // User chose to deny all remaining
}

// securityImplications maps tool names to their security implications
var securityImplications = map[string]string{
	// Write operations
	"write_file":       "Creates or overwrites a file on disk",
	"edit_file":        "Modifies existing file content",
	"append_file":      "Adds content to an existing file",
	"insert_lines":     "Inserts lines into a file",
	"replace_lines":    "Replaces specific lines in a file",
	"delete_lines":     "Removes lines from a file",
	"copy_file":        "Duplicates a file to a new location",
	"move_file":        "Moves or renames a file",
	"create_directory": "Creates a new directory",

	// Execute operations
	"execute_command": "Runs an arbitrary shell command",
	"docker_exec":     "Executes a command inside a container",
	"kubectl_exec":    "Executes a command inside a Kubernetes pod",

	// Git operations
	"git_add":    "Stages files for commit",
	"git_commit": "Creates a permanent commit in history",

	// Destructive operations
	"delete_file":       "Permanently removes a file",
	"kubectl_delete":    "Deletes Kubernetes resources",
	"kubectl_apply":     "Creates or modifies Kubernetes resources",
	"helm_install":      "Installs or upgrades Helm releases",
	"terraform_init":    "Initializes Terraform and downloads providers",
	"terraform_plan":    "Plans infrastructure changes (may query cloud APIs)",
	"terraform_apply":   "Applies changes to cloud infrastructure",
	"terraform_destroy": "Destroys cloud infrastructure resources",
	"docker_build":      "Builds a Docker image",
	"docker_compose":    "Manages multi-container Docker applications",
	"aws_cli":           "Executes AWS CLI commands (may modify resources)",
	"aws_ecs":           "Manages AWS ECS services",
	"aws_eks":           "Manages AWS EKS clusters",
	"az_cli":            "Executes Azure CLI commands",
	"az_aks":            "Manages Azure AKS clusters",
	"gcloud":            "Executes Google Cloud commands",
	"gke":               "Manages Google Kubernetes Engine clusters",
}

// GetSecurityImplication returns the security implication for a tool
func GetSecurityImplication(toolName string) string {
	if impl, ok := securityImplications[toolName]; ok {
		return impl
	}
	return "May modify system state"
}

// getCategoryStyle returns the appropriate style for a permission category
func getCategoryStyle(category permissions.PermissionCategory) (lipgloss.Style, string) {
	switch category {
	case permissions.CategoryDestructive:
		return SecurityDestructive, IconDanger
	case permissions.CategoryExecute:
		return SecurityExecute, IconWarning
	case permissions.CategoryWrite, permissions.CategoryGit:
		return SecurityWrite, IconLock
	default:
		return WarningStyle, IconInfo
	}
}

// getCategoryRiskLevel returns a human-readable risk level
func getCategoryRiskLevel(category permissions.PermissionCategory) string {
	switch category {
	case permissions.CategoryDestructive:
		return "HIGH RISK"
	case permissions.CategoryExecute:
		return "ELEVATED RISK"
	case permissions.CategoryWrite, permissions.CategoryGit:
		return "MODERATE RISK"
	default:
		return "LOW RISK"
	}
}

// renderSecurityAuditBox renders the security audit information box
func renderSecurityAuditBox(info *SecurityAuditInfo, batch *BatchAuditContext) {
	categoryStyle, categoryIcon := getCategoryStyle(info.Category)
	riskLevel := getCategoryRiskLevel(info.Category)

	// Build the content
	var content strings.Builder

	// Header with risk level
	header := fmt.Sprintf("%s SECURITY AUDIT - %s", IconShield, riskLevel)
	content.WriteString(SecurityAuditHeader.Render(header))
	content.WriteString("\n\n")

	// Batch indicator if applicable
	if batch != nil && batch.TotalTools > 1 {
		batchInfo := fmt.Sprintf("Operation %d of %d", batch.CurrentIndex, batch.TotalTools)
		content.WriteString(BatchIndicator.Render(batchInfo))
		content.WriteString("\n\n")
	}

	// Tool name with category badge
	content.WriteString(SecurityAuditLabel.Render("Tool:"))
	content.WriteString(" ")
	content.WriteString(SecurityAuditValue.Render(info.ToolName))
	content.WriteString("\n")

	// Category with styled badge
	content.WriteString(SecurityAuditLabel.Render("Category:"))
	content.WriteString(" ")
	content.WriteString(categoryStyle.Render(fmt.Sprintf(" %s %s ", categoryIcon, info.Category)))
	content.WriteString("\n")

	// Target if available
	target := extractTarget(info.Params)
	if target != "" {
		displayTarget := target
		if len(displayTarget) > 40 {
			displayTarget = displayTarget[:37] + "..."
		}
		content.WriteString(SecurityAuditLabel.Render("Target:"))
		content.WriteString(" ")
		content.WriteString(Subtle.Render(displayTarget))
		content.WriteString("\n")
	}

	content.WriteString("\n")

	// Security implication
	content.WriteString(SecurityImplication.Render(fmt.Sprintf("%s %s", IconWarning, info.Implication)))

	// Render the box
	fmt.Println()
	fmt.Println(SecurityAuditBox.Render(content.String()))
	fmt.Println()
}

// PromptSecurityAudit shows a security-focused confirmation prompt for audit-first enforcement
// Returns the user's choice
func PromptSecurityAudit(info *SecurityAuditInfo) SecurityAuditChoice {
	return PromptSecurityAuditBatch(info, nil)
}

// PromptSecurityAuditBatch shows a security audit prompt with batch options when multiple tools
// Returns the user's choice
func PromptSecurityAuditBatch(info *SecurityAuditInfo, batch *BatchAuditContext) SecurityAuditChoice {
	// Check if batch decision already made
	if batch != nil {
		if batch.AllowRemaining {
			return SecurityAuditAllow
		}
		if batch.DenyRemaining {
			return SecurityAuditDeny
		}
	}

	// Render the styled box
	renderSecurityAuditBox(info, batch)

	// Define choices
	type choice struct {
		Label string
		Short string
	}

	choices := []choice{
		{Label: "Yes, proceed with operation", Short: "y"},
		{Label: "No, cancel operation", Short: "n"},
		{Label: "Show full details", Short: "d"},
	}

	// Add batch options if multiple tools
	if batch != nil && batch.TotalTools > 1 {
		remaining := batch.TotalTools - batch.CurrentIndex
		if remaining > 0 {
			choices = append(choices,
				choice{Label: fmt.Sprintf("Allow all remaining (%d)", remaining), Short: "A"},
				choice{Label: fmt.Sprintf("Deny all remaining (%d)", remaining), Short: "D"},
			)
		}
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "\U0001F512 {{ .Label | yellow }}",
		Inactive: "   {{ .Label }}",
		Selected: "\U0001F512 {{ .Label | cyan }}",
	}

	prompt := promptui.Select{
		Label:     "Proceed?",
		Items:     choices,
		Templates: templates,
		Size:      len(choices),
		CursorPos: 1, // Default to "No" for safety
	}

	idx, _, err := prompt.Run()
	if err != nil {
		// User cancelled - treat as deny
		return SecurityAuditDeny
	}

	switch idx {
	case 0:
		return SecurityAuditAllow
	case 1:
		return SecurityAuditDeny
	case 2:
		return SecurityAuditDetails
	case 3:
		return SecurityAuditAllowAll
	case 4:
		return SecurityAuditDenyAll
	}

	return SecurityAuditDeny
}

// DisplaySecurityAuditDetails shows full details of the operation with enhanced formatting
func DisplaySecurityAuditDetails(info *SecurityAuditInfo) {
	categoryStyle, categoryIcon := getCategoryStyle(info.Category)

	fmt.Println()
	fmt.Println(Bold.Render("╭─ Operation Details ─────────────────────────────────────╮"))
	fmt.Println()

	// Basic info
	fmt.Printf("  %s %s\n", SecurityAuditLabel.Render("Tool:"), SecurityAuditValue.Render(info.ToolName))
	fmt.Printf("  %s %s\n", SecurityAuditLabel.Render("Category:"),
		categoryStyle.Render(fmt.Sprintf(" %s %s ", categoryIcon, info.Category)))
	fmt.Printf("  %s %s\n", SecurityAuditLabel.Render("Risk:"), getCategoryRiskLevel(info.Category))
	fmt.Println()

	// Implication
	fmt.Println(Bold.Render("  Security Implication:"))
	fmt.Printf("  %s\n", SecurityImplication.Render(info.Implication))
	fmt.Println()

	// Parameters
	if len(info.Params) > 0 {
		fmt.Println(Bold.Render("  Parameters:"))
		for k, v := range info.Params {
			str := fmt.Sprintf("%v", v)
			// For multi-line values, indent properly
			if strings.Contains(str, "\n") {
				lines := strings.Split(str, "\n")
				fmt.Printf("  %s:\n", Subtle.Render(k))
				for _, line := range lines {
					if len(line) > 70 {
						line = line[:67] + "..."
					}
					fmt.Printf("    %s\n", line)
				}
			} else {
				if len(str) > 60 {
					str = str[:57] + "..."
				}
				fmt.Printf("  %s: %s\n", Subtle.Render(k), str)
			}
		}
	}

	fmt.Println()
	fmt.Println(Bold.Render("╰──────────────────────────────────────────────────────────╯"))
	fmt.Println()
}

// DisplaySecurityAuditDenied shows a message when an operation is blocked by security audit
func DisplaySecurityAuditDenied(toolName string) {
	fmt.Println(WarningStyle.Render(fmt.Sprintf("%s Operation '%s' blocked by security audit", IconLock, toolName)))
}

// DisplaySecurityAuditBatchDenied shows a message when remaining operations are denied
func DisplaySecurityAuditBatchDenied(remaining int) {
	fmt.Println(WarningStyle.Render(fmt.Sprintf("%s Denied %d remaining operation(s) by security audit", IconLock, remaining)))
}

// DisplaySecurityAuditBatchAllowed shows a message when remaining operations are allowed
func DisplaySecurityAuditBatchAllowed(remaining int) {
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("%s Allowing %d remaining operation(s)", IconSuccess, remaining)))
}

// extractTarget extracts the target from common tool parameters
func extractTarget(params map[string]interface{}) string {
	// Check common parameter names for target
	targetKeys := []string{"file_path", "path", "target", "command", "file", "directory", "source"}
	for _, key := range targetKeys {
		if v, ok := params[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
