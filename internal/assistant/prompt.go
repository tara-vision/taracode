package assistant

import (
	"fmt"
	"os"
	"path/filepath"

	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/viper"
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
	a.systemPrompt = buildSystemPrompt(a.workingDir, a.storage, a.mode)
	// Update system message in conversation
	if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
		a.conversation[0].Content = a.systemPrompt
	}
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

// applyPolicyMode switches to the mode the policy file names. It goes through SetMode, so operate
// mode still needs a loadable policy and project storage; a refusal is shown and the session stays
// in its current mode.
func (a *Assistant) applyPolicyMode() {
	if a.policyErr != nil || a.pol.Mode == "" || a.pol.Mode == a.mode {
		return
	}
	if err := a.SetMode(a.pol.Mode); err != nil {
		fmt.Println(a.renderer.WarningMessage(
			fmt.Sprintf("The policy's %s mode was not applied, staying in %s mode: %v", a.pol.Mode, a.mode, err)))
	}
}

// buildSystemPrompt assembles the prompt: the persona with the mode line, TARACODE.md, memories,
// the active plan and the working directory.
func buildSystemPrompt(workingDir string, storageMgr *storage.Manager, mode policy.Mode) string {
	prompt := baseSystemPromptCompact + "\n\nMode: " + string(mode)

	// Check for TARACODE.md in current directory
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	content, err := os.ReadFile(taracodeFile) //nolint:gosec // reads TARACODE.md from the project's own working directory
	if err == nil {
		prompt += fmt.Sprintf("\n\n## PROJECT CONTEXT\nThe following is project-specific guidance from TARACODE.md:\n\n%s",
			string(content))
	}

	// Include relevant project memories if available
	if viper.GetBool("memory.enabled") {
		if memoryMgr := getMemoryManager(workingDir); memoryMgr != nil {
			maxTokens := viper.GetInt("memory.max_context_tokens")
			if maxTokens <= 0 {
				maxTokens = 2000
			}
			memories := memoryMgr.GetRelevantMemories("", maxTokens)
			if len(memories) > 0 {
				prompt += "\n\n## PROJECT MEMORIES\nRemembered facts about this project:\n\n"
				for _, mem := range memories {
					prompt += fmt.Sprintf("- [%s] %s\n", mem.Category, mem.Content)
					// Increment use count asynchronously to avoid blocking
					go func(id string) {
						_ = memoryMgr.IncrementUseCount(id)
					}(mem.ID)
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
