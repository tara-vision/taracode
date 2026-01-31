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

// FormatToolStatus returns styled tool execution status
func (r *Renderer) FormatToolStatus(tool string, params map[string]interface{}, result string, isError bool) string {
	if isError {
		return ToolError.Render(IconError + " " + tool + " failed")
	}

	switch tool {
	case "read_file":
		filePath, _ := params["file_path"].(string)
		lines := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Read %s (%d lines)", IconArrow, filepath.Base(filePath), lines))

	case "search_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No matches") {
			return ToolRead.Render(fmt.Sprintf("%s Searched for \"%s\" (no matches)", IconArrow, pattern))
		}
		return ToolRead.Render(fmt.Sprintf("%s Searched for \"%s\" (%d matches)", IconArrow, pattern, matches))

	case "list_files":
		dir, _ := params["directory"].(string)
		if dir == "" || dir == "." {
			dir = "current directory"
		}
		items := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Listed %s (%d items)", IconArrow, dir, items))

	case "execute_command":
		cmd, _ := params["command"].(string)
		if len(cmd) > MaxCommandDisplay {
			cmd = cmd[:MaxCommandDisplay-3] + "..."
		}
		return ToolRead.Render(fmt.Sprintf("%s Executed: %s", IconArrow, cmd))

	case "write_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Wrote %s", IconSuccess, filepath.Base(filePath)))

	case "append_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Appended to %s", IconSuccess, filepath.Base(filePath)))

	case "edit_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Edited %s", IconSuccess, filepath.Base(filePath)))

	case "insert_lines":
		filePath, _ := params["file_path"].(string)
		lineNum, _ := params["line_number"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Inserted at line %d in %s", IconSuccess, int(lineNum), filepath.Base(filePath)))

	case "replace_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Replaced lines %d-%d in %s", IconSuccess, int(startLine), int(endLine), filepath.Base(filePath)))

	case "delete_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Deleted lines %d-%d from %s", IconSuccess, int(startLine), int(endLine), filepath.Base(filePath)))

	case "copy_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Copied %s to %s", IconSuccess, filepath.Base(src), filepath.Base(dst)))

	case "move_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Moved %s to %s", IconSuccess, filepath.Base(src), filepath.Base(dst)))

	case "delete_file":
		filePath, _ := params["file_path"].(string)
		recursive, _ := params["recursive"].(bool)
		if recursive {
			return ToolWrite.Render(fmt.Sprintf("%s Deleted %s (recursive)", IconSuccess, filepath.Base(filePath)))
		}
		return ToolWrite.Render(fmt.Sprintf("%s Deleted %s", IconSuccess, filepath.Base(filePath)))

	case "create_directory":
		dirPath, _ := params["path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Created directory %s", IconSuccess, filepath.Base(dirPath)))

	case "find_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No files found") {
			return ToolRead.Render(fmt.Sprintf("%s Find \"%s\" (no matches)", IconArrow, pattern))
		}
		return ToolRead.Render(fmt.Sprintf("%s Find \"%s\" (%d files)", IconArrow, pattern, matches))

	case "git_status":
		if strings.Contains(result, "clean") {
			return ToolRead.Render(fmt.Sprintf("%s Git status: clean", IconArrow))
		}
		changes := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Git status: %d changes", IconArrow, changes))

	case "git_diff":
		if strings.Contains(result, "No changes") {
			return ToolRead.Render(fmt.Sprintf("%s Git diff: no changes", IconArrow))
		}
		lines := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Git diff: %d lines", IconArrow, lines))

	case "git_log":
		commits := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Git log: %d commits", IconArrow, commits))

	case "git_add":
		return ToolWrite.Render(fmt.Sprintf("%s Git: staged files", IconSuccess))

	case "git_commit":
		return ToolWrite.Render(fmt.Sprintf("%s Git: commit created", IconSuccess))

	case "git_branch":
		branches := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Git branches: %d", IconArrow, branches))

	case "web_search":
		query, _ := params["query"].(string)
		if len(query) > 40 {
			query = query[:40] + "..."
		}
		if strings.Contains(result, "No results found") {
			return ToolRead.Render(fmt.Sprintf("%s Searched \"%s\" (no results)", IconArrow, query))
		}
		// Count results by counting numbered items (1. 2. etc.)
		resultCount := strings.Count(result, "\n1.") + strings.Count(result, "\n2.") + strings.Count(result, "\n3.") + strings.Count(result, "\n4.") + strings.Count(result, "\n5.")
		if resultCount == 0 && strings.Contains(result, "Quick Answer") {
			return ToolRead.Render(fmt.Sprintf("%s Searched \"%s\" (found answer)", IconArrow, query))
		}
		return ToolRead.Render(fmt.Sprintf("%s Searched \"%s\" (%d results)", IconArrow, query, resultCount))

	case "web_fetch":
		urlStr, _ := params["url"].(string)
		// Extract domain from URL for display
		if len(urlStr) > 50 {
			urlStr = urlStr[:50] + "..."
		}
		// Get content length from result
		if strings.Contains(result, "Content length:") {
			return ToolRead.Render(fmt.Sprintf("%s Fetched %s", IconArrow, urlStr))
		}
		return ToolRead.Render(fmt.Sprintf("%s Fetched %s", IconArrow, urlStr))

	// Kubernetes tools
	case "kubectl_get":
		resource, _ := params["resource"].(string)
		namespace, _ := params["namespace"].(string)
		if namespace != "" {
			return ToolRead.Render(fmt.Sprintf("%s kubectl get %s -n %s", IconArrow, resource, namespace))
		}
		return ToolRead.Render(fmt.Sprintf("%s kubectl get %s", IconArrow, resource))

	case "kubectl_apply":
		file, _ := params["file"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s kubectl apply -f %s", IconSuccess, filepath.Base(file)))

	case "kubectl_delete":
		resource, _ := params["resource"].(string)
		name, _ := params["name"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s kubectl delete %s %s", IconSuccess, resource, name))

	case "kubectl_describe":
		resource, _ := params["resource"].(string)
		name, _ := params["name"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubectl describe %s %s", IconArrow, resource, name))

	case "kubectl_logs":
		pod, _ := params["pod"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubectl logs %s", IconArrow, pod))

	case "kubectl_exec":
		pod, _ := params["pod"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubectl exec %s", IconArrow, pod))

	case "helm_list":
		namespace, _ := params["namespace"].(string)
		if namespace != "" {
			return ToolRead.Render(fmt.Sprintf("%s helm list -n %s", IconArrow, namespace))
		}
		return ToolRead.Render(fmt.Sprintf("%s helm list", IconArrow))

	case "helm_install":
		release, _ := params["release"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s helm install %s", IconSuccess, release))

	// Terraform tools
	case "terraform_init":
		return ToolWrite.Render(fmt.Sprintf("%s terraform init", IconSuccess))

	case "terraform_plan":
		return ToolRead.Render(fmt.Sprintf("%s terraform plan", IconArrow))

	case "terraform_apply":
		return ToolWrite.Render(fmt.Sprintf("%s terraform apply", IconSuccess))

	case "terraform_destroy":
		return ToolWrite.Render(fmt.Sprintf("%s terraform destroy", IconSuccess))

	case "terraform_output":
		name, _ := params["name"].(string)
		if name != "" {
			return ToolRead.Render(fmt.Sprintf("%s terraform output %s", IconArrow, name))
		}
		return ToolRead.Render(fmt.Sprintf("%s terraform output", IconArrow))

	case "terraform_state":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s terraform state %s", IconArrow, subcommand))

	// Docker tools
	case "docker_build":
		tag, _ := params["tag"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s docker build -t %s", IconSuccess, tag))

	case "docker_ps":
		return ToolRead.Render(fmt.Sprintf("%s docker ps", IconArrow))

	case "docker_logs":
		container, _ := params["container"].(string)
		return ToolRead.Render(fmt.Sprintf("%s docker logs %s", IconArrow, container))

	case "docker_compose":
		subcommand, _ := params["subcommand"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s docker compose %s", IconSuccess, subcommand))

	case "docker_exec":
		container, _ := params["container"].(string)
		return ToolRead.Render(fmt.Sprintf("%s docker exec %s", IconArrow, container))

	// AWS tools
	case "aws_cli":
		service, _ := params["service"].(string)
		command, _ := params["command"].(string)
		return ToolRead.Render(fmt.Sprintf("%s aws %s %s", IconArrow, service, command))

	case "aws_ecs":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s aws ecs %s", IconArrow, subcommand))

	case "aws_eks":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s aws eks %s", IconArrow, subcommand))

	// Azure tools
	case "az_cli":
		group, _ := params["group"].(string)
		command, _ := params["command"].(string)
		return ToolRead.Render(fmt.Sprintf("%s az %s %s", IconArrow, group, command))

	case "az_aks":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s az aks %s", IconArrow, subcommand))

	// GCP tools
	case "gcloud":
		component, _ := params["component"].(string)
		return ToolRead.Render(fmt.Sprintf("%s gcloud %s", IconArrow, component))

	case "gke":
		subcommand, _ := params["subcommand"].(string)
		return ToolRead.Render(fmt.Sprintf("%s gcloud container %s", IconArrow, subcommand))

	// Security tools
	case "trivy_scan":
		target, _ := params["target"].(string)
		scanType, _ := params["type"].(string)
		if scanType == "" {
			scanType = "image"
		}
		return ToolRead.Render(fmt.Sprintf("%s trivy %s scan: %s", IconArrow, scanType, target))

	case "gitleaks_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s gitleaks scan: %s", IconArrow, path))

	case "secrets_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s secrets scan: %s", IconArrow, path))

	case "dependency_audit":
		auditType, _ := params["type"].(string)
		return ToolRead.Render(fmt.Sprintf("%s %s dependency audit", IconArrow, auditType))

	case "sast_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s SAST scan: %s", IconArrow, path))

	case "tfsec_scan":
		path, _ := params["path"].(string)
		if path == "" || path == "." {
			path = "current directory"
		}
		return ToolRead.Render(fmt.Sprintf("%s tfsec scan: %s", IconArrow, path))

	case "kubesec_scan":
		file, _ := params["file"].(string)
		return ToolRead.Render(fmt.Sprintf("%s kubesec scan: %s", IconArrow, filepath.Base(file)))

	default:
		return ToolRead.Render(fmt.Sprintf("%s %s completed", IconArrow, tool))
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
