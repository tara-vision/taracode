package assistant

import (
	"fmt"
	"os"
	"path/filepath"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
)

// baseSystemPromptCompact is the persona; the tool schemas travel in the request's tools field.
const baseSystemPromptCompact = `You are Tara Code, a DevOps & Cloud AI assistant specialized in infrastructure ` +
	`automation, container orchestration, and cloud platforms.

## DEVOPS EXPERTISE

You have deep expertise in:
- Infrastructure as Code (Terraform, CloudFormation, Ansible, Pulumi)
- Container Orchestration (Kubernetes, Docker, ECS/EKS/AKS/GKE)
- CI/CD & GitOps (GitHub Actions, GitLab CI, ArgoCD, Flux)
- Cloud Platforms (AWS, Azure, GCP)
- Monitoring & Observability (Prometheus, Grafana, CloudWatch)
- Security & Compliance (RBAC, Pod Security, Secrets management)

## CRITICAL RULE - DATE AND TIME

When the user asks about the current date, time, day of week, or anything like "what day is today":
- You MUST call the get_datetime tool
- You MUST then tell the user the result (e.g., "Today is Saturday, February 7, 2026.")
- NEVER guess the date from your training data - it will be wrong
- NEVER use web_search or execute_command for this - use get_datetime

## BEHAVIOR

1. Use tools to accomplish tasks - read files before editing, validate before applying
2. For destructive operations (destroy, delete), always confirm with user first
3. Be concise - after tool execution, confirm briefly what was done
4. Consider security implications in all recommendations`

// RefreshSystemPrompt rebuilds the system prompt to include any new memories or context
func (a *Assistant) RefreshSystemPrompt() {
	a.systemPrompt = buildSystemPrompt(a.workingDir, a.storage, a.mode, a.memoryBudget())
	// Update system message in conversation
	if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
		a.conversation[0].Content = a.systemPrompt
	}
}

// memoryBudget is the token budget buildSystemPrompt gets for project memories: 0 (memories off)
// when memory.enabled is false, a.memoryMaxTokens otherwise.
func (a *Assistant) memoryBudget() int {
	if !a.memoryEnabled {
		return 0
	}
	return a.memoryMaxTokens
}

// SetMode switches the operating mode, the exposed tools and the system prompt. Operate needs a
// loadable policy and project storage (the audit log and the permission store live there).
func (a *Assistant) SetMode(mode policy.Mode) error {
	if mode != policy.ModeInvestigate && mode != policy.ModeOperate {
		return fmt.Errorf("invalid mode %q (investigate or operate)", mode)
	}
	if mode == policy.ModeOperate {
		if a.policyErr != nil {
			return fmt.Errorf("operate mode is locked: %w", a.policyErr)
		}
		if a.storage == nil {
			return fmt.Errorf("operate mode needs an initialised project (run /init)")
		}
	}
	a.mode = mode
	a.refreshTools()
	a.RefreshSystemPrompt()
	return nil
}

// applyStartupMode resolves the mode New starts a session in and applies it through SetMode: the
// --mode flag (opts.Mode) wins outright, then a policy file's own mode - only when the effective
// policy actually came from a file rather than the built-in default, whose Mode is always
// "investigate" - then the configured default (opts.DefaultMode), else investigate. A refusal
// (operate without project storage, or a policy that failed to load) is warned and the session
// stays in its current mode; a.mode is never assigned outside SetMode.
func (a *Assistant) applyStartupMode(opts Options) {
	target := policy.ModeInvestigate
	switch {
	case opts.Mode != "":
		target = opts.Mode
	case a.policyErr == nil && len(a.policySources) > 0 && a.policySources[0] != "built-in" && a.pol.Mode != "":
		target = a.pol.Mode
	case opts.DefaultMode != "":
		target = opts.DefaultMode
	}
	if target == a.mode {
		return
	}
	if err := a.SetMode(target); err != nil {
		fmt.Println(a.renderer.WarningMessage(
			fmt.Sprintf("The %s mode was not applied, staying in %s mode: %v", target, a.mode, err)))
	}
}

// buildSystemPrompt assembles the prompt: the persona with the mode line, TARACODE.md, memories,
// the active plan and the working directory. memoryMaxTokens is the token budget for the project
// memories section; 0 leaves it out entirely.
func buildSystemPrompt(workingDir string, storageMgr *storage.Manager, mode policy.Mode, memoryMaxTokens int) string {
	prompt := baseSystemPromptCompact + "\n\nMode: " + string(mode)

	// Check for TARACODE.md in current directory
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	content, err := os.ReadFile(taracodeFile) //nolint:gosec // reads TARACODE.md from the project's own working directory
	if err == nil {
		prompt += fmt.Sprintf("\n\n## PROJECT CONTEXT\nThe following is project-specific guidance from TARACODE.md:\n\n%s",
			string(content))
	}

	// Include relevant project memories if available
	if memoryMaxTokens > 0 {
		if memoryMgr := getMemoryManager(workingDir); memoryMgr != nil {
			memories := memoryMgr.GetRelevantMemories("", memoryMaxTokens)
			if len(memories) > 0 {
				prompt += "\n\n## PROJECT MEMORIES\nRemembered facts about this project:\n\n"
				for _, mem := range memories {
					prompt += fmt.Sprintf("- [%s] %s\n", mem.Category, mem.Content)
				}
				// Record use once the prompt text is assembled: synchronous, not fire-and-forget, so
				// a failure or a slow store is never silently lost (Task 13 triage finding).
				for _, mem := range memories {
					_ = memoryMgr.IncrementUseCount(mem.ID)
				}
			}
		}
	}

	// Include active plan if exists
	if storageMgr != nil {
		if plan, err := storageMgr.GetActivePlan(); err == nil && plan != nil {
			prompt += "\n\n## ACTIVE PLAN\n"
			prompt += fmt.Sprintf("**%s**\n", plan.Title)
			for i, task := range plan.Tasks {
				status := "[ ]"
				switch task.Status {
				case storage.TaskStatusCompleted:
					status = "[x]"
				case storage.TaskStatusInProgress:
					status = "[>]"
				}
				prompt += fmt.Sprintf("%d. %s %s\n", i+1, status, task.Content)
			}
			prompt += "\nUpdate task status as you complete them."
		}
	}

	// Add working directory context
	prompt += fmt.Sprintf("\n\nCurrent working directory: %s", workingDir)

	return prompt
}

// getMemoryManager creates a memory manager for the given working directory
// Returns nil if the project is not initialized or memory is not available
func getMemoryManager(workingDir string) *memory.Manager {
	taracodeDir := filepath.Join(workingDir, ".taracode")
	if _, err := os.Stat(taracodeDir); os.IsNotExist(err) {
		return nil
	}
	mm, err := memory.NewManager(taracodeDir)
	if err != nil {
		return nil
	}
	return mm
}
