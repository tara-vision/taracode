package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
)

// Config holds UI configuration options
type Config struct {
	EnableColor    bool
	EnableSpinner  bool
	EnableMarkdown bool
}

// DefaultConfig returns the default UI configuration
func DefaultConfig() *Config {
	return &Config{
		EnableColor:    true,
		EnableSpinner:  true,
		EnableMarkdown: true,
	}
}

// Renderer handles all UI output formatting
type Renderer struct {
	config *Config
}

// NewRenderer creates a new renderer with default config
func NewRenderer() *Renderer {
	return &Renderer{
		config: DefaultConfig(),
	}
}

// NewRendererWithConfig creates a renderer with custom config
func NewRendererWithConfig(config *Config) *Renderer {
	return &Renderer{
		config: config,
	}
}

// WelcomeMessage returns the styled welcome banner
func (r *Renderer) WelcomeMessage(mode storage.OperatingMode) string {
	var sb strings.Builder

	if mode == storage.ModeSecurity {
		// Security mode gets a prominent banner
		sb.WriteString(r.SecurityModeBanner())
	} else {
		// Standard DevOps mode
		title := TitleStyle.Render(IconCloud + " Tara Code")
		subtitle := Subtle.Render("DevOps & Cloud AI Assistant")
		sb.WriteString(fmt.Sprintf("%s - %s\n", title, subtitle))
		sb.WriteString(Subtle.Render("Type '/help' for commands, 'exit' to quit"))
		sb.WriteString("\n")
	}

	return sb.String()
}

// SecurityModeBanner returns a prominent security mode banner
func (r *Renderer) SecurityModeBanner() string {
	var sb strings.Builder

	// Build banner content
	var content strings.Builder

	// Title line with shield icons
	title := SecurityModeTitle.Render(IconShield + " SECURITY MODE " + IconShield)
	content.WriteString(title + "\n")

	// Subtitle
	subtitle := SecurityModeSubtitle.Render("DevSecOps Assistant - Audit-First Protection")
	content.WriteString(subtitle + "\n\n")

	// Feature bullets
	bullets := []string{
		IconLock + " All write/execute operations require confirmation",
		IconShield + " Audit log tracks all security decisions",
		IconDanger + " Destructive operations highlighted with risk level",
	}

	for _, bullet := range bullets {
		content.WriteString(SecurityModeBullet.Render(bullet) + "\n")
	}

	content.WriteString("\n")
	content.WriteString(Subtle.Render("Type '/help' for commands, '/audit' to view audit log"))

	// Wrap in banner box
	sb.WriteString(SecurityModeBannerBox.Render(content.String()))
	sb.WriteString("\n")

	return sb.String()
}

// SecurityModeActivatedMessage returns a message shown when switching to security mode
func (r *Renderer) SecurityModeActivatedMessage() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(SecurityModeTitle.Render(IconShield + " Security Mode Activated"))
	sb.WriteString("\n\n")

	sb.WriteString(SecurityModeBullet.Render("  " + IconLock + " Audit-first enforcement is now active"))
	sb.WriteString("\n")
	sb.WriteString(SecurityModeBullet.Render("  " + IconShield + " All operations will be logged to audit trail"))
	sb.WriteString("\n")
	sb.WriteString(Subtle.Render("  Use '/audit' to view the security audit log"))
	sb.WriteString("\n")

	return sb.String()
}

// SecurityModeDeactivatedMessage returns a message shown when switching from security mode
func (r *Renderer) SecurityModeDeactivatedMessage() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(SessionStyle.Render(IconCloud + " DevOps Mode Activated"))
	sb.WriteString("\n")
	sb.WriteString(Subtle.Render("  Standard permission-based tool execution"))
	sb.WriteString("\n")

	return sb.String()
}

// ProjectContextMessage returns styled project context info
func (r *Renderer) ProjectContextMessage(loaded bool) string {
	if loaded {
		return SuccessStyle.Render(IconFolder+" Project context loaded from TARACODE.md") + "\n"
	}
	return WarningStyle.Render(IconTip+" Run '/init' to initialize project context") + "\n"
}

// SessionResumeMessage returns styled session info
func (r *Renderer) SessionResumeMessage(messageCount int) string {
	var sb strings.Builder
	sb.WriteString(SessionStyle.Render(fmt.Sprintf("%s Resuming session with %d previous messages", IconSession, messageCount)))
	sb.WriteString("\n")
	sb.WriteString(Subtle.Render("   Type '/session new' to start fresh"))
	sb.WriteString("\n")
	return sb.String()
}

// formatDuration returns a human-readable duration suffix for tool execution.
// Returns empty string for durations under 1 second to reduce noise.
func formatDuration(durationMs int64) string {
	if durationMs < 1000 {
		return ""
	}
	seconds := float64(durationMs) / 1000.0
	if seconds < 60 {
		return fmt.Sprintf(" [%.1fs]", seconds)
	}
	minutes := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf(" [%dm%ds]", minutes, secs)
}

// FormatToolStatus returns styled tool execution status
func (r *Renderer) FormatToolStatus(tool string, params map[string]interface{}, result string, isError bool) string {
	return r.FormatToolStatusWithDuration(tool, params, result, isError, 0)
}

// FormatToolStatusWithDuration returns styled tool execution status with optional duration
func (r *Renderer) FormatToolStatusWithDuration(tool string, params map[string]interface{}, result string, isError bool, durationMs int64) string {
	dur := formatDuration(durationMs)

	if isError {
		return ToolError.Render(IconError + " " + tool + " failed" + dur)
	}

	switch tool {
	case "read_file":
		filePath, _ := params["file_path"].(string)
		lines := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Read %s (%d lines)%s", IconArrow, filepath.Base(filePath), lines, dur))

	case "search_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No matches") {
			return ToolRead.Render(fmt.Sprintf("%s Searched for \"%s\" (no matches)%s", IconArrow, pattern, dur))
		}
		return ToolRead.Render(fmt.Sprintf("%s Searched for \"%s\" (%d matches)%s", IconArrow, pattern, matches, dur))

	case "list_files":
		dir, _ := params["directory"].(string)
		if dir == "" || dir == "." {
			dir = "current directory"
		}
		items := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Listed %s (%d items)%s", IconArrow, dir, items, dur))

	case "execute_command":
		cmd, _ := params["command"].(string)
		if len(cmd) > MaxCommandDisplay {
			cmd = cmd[:MaxCommandDisplay-3] + "..."
		}
		return ToolRead.Render(fmt.Sprintf("%s Executed: %s%s", IconArrow, cmd, dur))

	case "write_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Wrote %s%s", IconSuccess, filepath.Base(filePath), dur))

	case "append_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Appended to %s%s", IconSuccess, filepath.Base(filePath), dur))

	case "edit_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Edited %s%s", IconSuccess, filepath.Base(filePath), dur))

	case "insert_lines":
		filePath, _ := params["file_path"].(string)
		lineNum, _ := params["line_number"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Inserted at line %d in %s%s", IconSuccess, int(lineNum), filepath.Base(filePath), dur))

	case "replace_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Replaced lines %d-%d in %s%s", IconSuccess, int(startLine), int(endLine), filepath.Base(filePath), dur))

	case "delete_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Deleted lines %d-%d from %s%s", IconSuccess, int(startLine), int(endLine), filepath.Base(filePath), dur))

	case "copy_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Copied %s to %s%s", IconSuccess, filepath.Base(src), filepath.Base(dst), dur))

	case "move_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Moved %s to %s%s", IconSuccess, filepath.Base(src), filepath.Base(dst), dur))

	case "delete_file":
		filePath, _ := params["file_path"].(string)
		recursive, _ := params["recursive"].(bool)
		if recursive {
			return ToolWrite.Render(fmt.Sprintf("%s Deleted %s (recursive)%s", IconSuccess, filepath.Base(filePath), dur))
		}
		return ToolWrite.Render(fmt.Sprintf("%s Deleted %s%s", IconSuccess, filepath.Base(filePath), dur))

	case "create_directory":
		dirPath, _ := params["path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Created directory %s%s", IconSuccess, filepath.Base(dirPath), dur))

	case "find_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No files found") {
			return ToolRead.Render(fmt.Sprintf("%s Find \"%s\" (no matches)%s", IconArrow, pattern, dur))
		}
		return ToolRead.Render(fmt.Sprintf("%s Find \"%s\" (%d files)%s", IconArrow, pattern, matches, dur))

	case "git_status":
		if strings.Contains(result, "clean") {
			return ToolRead.Render(fmt.Sprintf("%s Git status: clean%s", IconArrow, dur))
		}
		changes := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Git status: %d changes%s", IconArrow, changes, dur))

	case "git_diff":
		if strings.Contains(result, "No changes") {
			return ToolRead.Render(fmt.Sprintf("%s Git diff: no changes%s", IconArrow, dur))
		}
		lines := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Git diff: %d lines%s", IconArrow, lines, dur))

	case "git_log":
		commits := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Git log: %d commits%s", IconArrow, commits, dur))

	case "git_add":
		return ToolWrite.Render(fmt.Sprintf("%s Git: staged files%s", IconSuccess, dur))

	case "git_commit":
		return ToolWrite.Render(fmt.Sprintf("%s Git: commit created%s", IconSuccess, dur))

	case "git_branch":
		branches := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Git branches: %d%s", IconArrow, branches, dur))

	case "web_search":
		query, _ := params["query"].(string)
		if len(query) > 40 {
			query = query[:40] + "..."
		}
		if strings.Contains(result, "No results found") {
			return ToolRead.Render(fmt.Sprintf("%s Searched \"%s\" (no results)%s", IconArrow, query, dur))
		}
		// Count results by counting numbered items (1. 2. etc.)
		resultCount := strings.Count(result, "\n1.") + strings.Count(result, "\n2.") + strings.Count(result, "\n3.") + strings.Count(result, "\n4.") + strings.Count(result, "\n5.")
		if resultCount == 0 && strings.Contains(result, "Quick Answer") {
			return ToolRead.Render(fmt.Sprintf("%s Searched \"%s\" (found answer)%s", IconArrow, query, dur))
		}
		return ToolRead.Render(fmt.Sprintf("%s Searched \"%s\" (%d results)%s", IconArrow, query, resultCount, dur))

	case "web_fetch":
		urlStr, _ := params["url"].(string)
		if len(urlStr) > 50 {
			urlStr = urlStr[:50] + "..."
		}
		return ToolRead.Render(fmt.Sprintf("%s Fetched %s%s", IconArrow, urlStr, dur))

	// Kubernetes tools
	case "kubectl_get":
		resource, _ := params["resource"].(string)
		namespace, _ := params["namespace"].(string)
		if namespace != "" {
			return ToolRead.Render(fmt.Sprintf("%s kubectl get %s -n %s%s", IconArrow, resource, namespace, dur))
		}
		return ToolRead.Render(fmt.Sprintf("%s kubectl get %s%s", IconArrow, resource, dur))

	case "kubectl_apply":
		file, _ := params["file"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s kubectl apply -f %s%s", IconSuccess, filepath.Base(file), dur))

	case "kubectl_delete":
		resource, _ := params["resource"].(string)
		name, _ := params["name"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s kubectl delete %s %s%s", IconSuccess, resource, name, dur))

	case "kubectl_describe":
		resource, _ := params["resource"].(string)
		name, _ := params["name"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubectl describe %s %s%s", IconArrow, resource, name, dur))

	case "kubectl_logs":
		pod, _ := params["pod"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubectl logs %s%s", IconArrow, pod, dur))

	case "kubectl_exec":
		pod, _ := params["pod"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubectl exec %s%s", IconArrow, pod, dur))

	case "helm_list":
		namespace, _ := params["namespace"].(string)
		if namespace != "" {
			return ToolRead.Render(fmt.Sprintf("%s helm list -n %s%s", IconArrow, namespace, dur))
		}
		return ToolRead.Render(fmt.Sprintf("%s helm list%s", IconArrow, dur))

	case "helm_install":
		release, _ := params["release"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s helm install %s%s", IconSuccess, release, dur))

	// Terraform tools
	case "terraform_init":
		return ToolWrite.Render(fmt.Sprintf("%s terraform init%s", IconSuccess, dur))

	case "terraform_plan":
		return ToolRead.Render(fmt.Sprintf("%s terraform plan%s", IconArrow, dur))

	case "terraform_apply":
		return ToolWrite.Render(fmt.Sprintf("%s terraform apply%s", IconSuccess, dur))

	case "terraform_destroy":
		return ToolWrite.Render(fmt.Sprintf("%s terraform destroy%s", IconSuccess, dur))

	case "terraform_output":
		name, _ := params["name"].(string)
		if name != "" {
			return ToolRead.Render(fmt.Sprintf("%s terraform output %s%s", IconArrow, name, dur))
		}
		return ToolRead.Render(fmt.Sprintf("%s terraform output%s", IconArrow, dur))

	case "terraform_state":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s terraform state %s%s", IconArrow, subcommand, dur))

	// Docker tools
	case "docker_build":
		tag, _ := params["tag"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s docker build -t %s%s", IconSuccess, tag, dur))

	case "docker_ps":
		return ToolRead.Render(fmt.Sprintf("%s docker ps%s", IconArrow, dur))

	case "docker_logs":
		container, _ := params["container"].(string)
		return ToolRead.Render(fmt.Sprintf("%s docker logs %s%s", IconArrow, container, dur))

	case "docker_compose":
		subcommand, _ := params["subcommand"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s docker compose %s%s", IconSuccess, subcommand, dur))

	case "docker_exec":
		container, _ := params["container"].(string)
		return ToolRead.Render(fmt.Sprintf("%s docker exec %s%s", IconArrow, container, dur))

	// AWS tools
	case "aws_cli":
		service, _ := params["service"].(string)
		command, _ := params["command"].(string)
		return ToolRead.Render(fmt.Sprintf("%s aws %s %s%s", IconArrow, service, command, dur))

	case "aws_ecs":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s aws ecs %s%s", IconArrow, subcommand, dur))

	case "aws_eks":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s aws eks %s%s", IconArrow, subcommand, dur))

	// Azure tools
	case "az_cli":
		group, _ := params["group"].(string)
		command, _ := params["command"].(string)
		return ToolRead.Render(fmt.Sprintf("%s az %s %s%s", IconArrow, group, command, dur))

	case "az_aks":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s az aks %s%s", IconArrow, subcommand, dur))

	// GCP tools
	case "gcloud":
		component, _ := params["component"].(string)
		return ToolRead.Render(fmt.Sprintf("%s gcloud %s%s", IconArrow, component, dur))

	case "gke":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s gcloud container %s%s", IconArrow, subcommand, dur))

	// Security tools
	case "trivy_scan":
		target, _ := params["target"].(string)
		scanType, _ := params["type"].(string)
		if scanType == "" {
			scanType = "image"
		}
		return ToolRead.Render(fmt.Sprintf("%s trivy %s scan: %s%s", IconArrow, scanType, target, dur))

	case "gitleaks_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s gitleaks scan: %s%s", IconArrow, path, dur))

	case "secrets_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s secrets scan: %s%s", IconArrow, path, dur))

	case "dependency_audit":
		auditType, _ := params["type"].(string)
		return ToolRead.Render(fmt.Sprintf("%s %s dependency audit%s", IconArrow, auditType, dur))

	case "sast_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s SAST scan: %s%s", IconArrow, path, dur))

	case "tfsec_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s tfsec scan: %s%s", IconArrow, path, dur))

	case "kubesec_scan":
		file, _ := params["file"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubesec scan: %s%s", IconArrow, filepath.Base(file), dur))

	default:
		return ToolRead.Render(fmt.Sprintf("%s %s completed%s", IconArrow, tool, dur))
	}
}

// PromptString returns the styled prompt
func (r *Renderer) PromptString() string {
	return PromptStyle.Render("❯") + " "
}

// PromptStringWithMode returns the styled prompt with mode indicator
func (r *Renderer) PromptStringWithMode(mode storage.OperatingMode) string {
	if mode == storage.ModeSecurity {
		return SecurityModePrompt.Render(IconShield) + " " + PromptStyle.Render("❯") + " "
	}
	return PromptStyle.Render("❯") + " "
}

// ErrorMessage formats an error message
func (r *Renderer) ErrorMessage(err error) string {
	return ToolError.Render(fmt.Sprintf("%s Error: %v", IconError, err))
}

// WarningMessage formats a warning message
func (r *Renderer) WarningMessage(msg string) string {
	return WarningStyle.Render(fmt.Sprintf("%s %s", IconWarning, msg))
}

// InfoMessage formats an info message
func (r *Renderer) InfoMessage(msg string) string {
	return SessionStyle.Render(fmt.Sprintf("%s %s", IconInfo, msg))
}

// SuccessMessage formats a success message
func (r *Renderer) SuccessMessage(msg string) string {
	return SuccessStyle.Render(fmt.Sprintf("%s %s", IconSuccess, msg))
}

// FormatUsage formats token usage statistics for display
func (r *Renderer) FormatUsage(usage *storage.TokenUsage) string {
	if usage == nil || usage.TotalTokens == 0 {
		return Subtle.Render("No token usage recorded yet.")
	}

	var sb strings.Builder
	sb.WriteString(SessionStyle.Render(IconInfo+" Token Usage") + "\n")
	sb.WriteString(fmt.Sprintf("  Prompt tokens:     %d\n", usage.PromptTokens))
	sb.WriteString(fmt.Sprintf("  Completion tokens: %d\n", usage.CompletionTokens))
	sb.WriteString(fmt.Sprintf("  Total tokens:      %d\n", usage.TotalTokens))

	return sb.String()
}

// ProviderMessage formats provider information for display
func (r *Renderer) ProviderMessage(info *provider.Info) string {
	if info == nil {
		return ""
	}
	return SuccessStyle.Render(fmt.Sprintf("%s Connected to %s", IconSuccess, info.Name)) + "\n"
}

// SearchFallbackMessage formats a search provider fallback notification
func (r *Renderer) SearchFallbackMessage(from, to string, reason error) string {
	var reasonStr string
	if reason != nil {
		errMsg := reason.Error()
		// Simplify common error messages
		switch {
		case strings.Contains(errMsg, "429") || strings.Contains(strings.ToLower(errMsg), "rate limit"):
			reasonStr = "rate limited"
		case strings.Contains(strings.ToLower(errMsg), "timeout"):
			reasonStr = "timed out"
		case strings.Contains(strings.ToLower(errMsg), "connection"):
			reasonStr = "connection error"
		default:
			// Truncate long error messages
			if len(errMsg) > 30 {
				reasonStr = errMsg[:27] + "..."
			} else {
				reasonStr = errMsg
			}
		}
	}

	msg := fmt.Sprintf("Search: %s %s %s", from, IconArrow, to)
	if reasonStr != "" {
		msg += fmt.Sprintf(" (%s)", reasonStr)
	}
	return WarningStyle.Render(fmt.Sprintf("%s %s", IconWarning, msg))
}
