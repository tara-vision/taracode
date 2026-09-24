package agent

import (
	"fmt"
	"os"
	"path/filepath"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
)

// personaPrompt is the one system prompt. The mode line, the project files, memories, the active
// plan and the working directory are appended at build time. It is split into concatenated
// literals (rather than one multi-line raw string) only to keep source lines under the repo's
// line-length limit; the assembled value is the text spec 4 specifies, unchanged.
const personaPrompt = "You are taracode, a local-first DevOps operator running on the user's machine. You " +
	"investigate Kubernetes, Helm, Terraform, Docker and cloud (AWS, Azure, GCP) " +
	"environments with the tools you are given and, only when the user has switched to " +
	"operate mode, you change them through a policy the user controls.\n" +
	"\n" +
	"Working rules:\n" +
	"- Look before you conclude: read files, describe resources, check logs, events and " +
	"plans. Report what the evidence shows and say plainly when you could not verify " +
	"something.\n" +
	"- Prefer the dedicated tools (kubectl, helm, terraform, docker, cloud, git) for those " +
	"CLIs and shell for everything else. One tool call per step; keep outputs small: name " +
	"resources, use selectors and namespaces, limit lines.\n" +
	"- Never invent resources, versions or command output. When a tool fails or is " +
	"blocked, say what happened and try a different angle or ask the user.\n" +
	"- Answer in short, structured Markdown: findings first, then the evidence, then next " +
	"steps. State risks (data loss, downtime, cost) before any change.\n" +
	"- Secrets in tool output appear as [redacted:kind]; never try to reconstruct them."

const investigateLine = "Mode: investigate. Every tool call must be read-only; tools that could change " +
	"anything are hidden or refused. Diagnose, explain and propose; do not ask the user to " +
	"switch modes unless they ask for a change."

const operateLine = "Mode: operate. Mutating calls go through the user's policy: they may be denied, need " +
	"a dry run first, or need approval. Prefer a plan or a dry run before a change and " +
	"explain what will change and why."

// maxContextFileBytes caps TARACODE.md and AGENTS.md in the prompt.
const maxContextFileBytes = 16 * 1024

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
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(
			fmt.Sprintf("The %s mode was not applied, staying in %s mode: %v", target, a.mode, err)))
	}
}

// contextFile reads name from workingDir and reports whether it was found. Content past
// maxContextFileBytes is cut with a trailing note, so one oversized file cannot blow the prompt's
// token budget.
func contextFile(workingDir, name string) (string, bool) {
	content, err := os.ReadFile(filepath.Join(workingDir, name)) //nolint:gosec // project-relative path
	if err != nil {
		return "", false
	}
	if len(content) <= maxContextFileBytes {
		return string(content), true
	}
	return string(content[:maxContextFileBytes]) +
		fmt.Sprintf("\n[truncated: the first 16 KB of %s are shown]", name), true
}

// buildSystemPrompt assembles the prompt: the persona with the mode line, TARACODE.md, AGENTS.md,
// memories, the active plan and the working directory. memoryMaxTokens is the token budget for the
// project memories section; 0 leaves it out entirely.
func buildSystemPrompt(workingDir string, storageMgr *storage.Manager, mode policy.Mode, memoryMaxTokens int) string {
	modeLine := investigateLine
	if mode == policy.ModeOperate {
		modeLine = operateLine
	}
	prompt := personaPrompt + "\n\n" + modeLine

	if content, ok := contextFile(workingDir, "TARACODE.md"); ok {
		prompt += fmt.Sprintf("\n\n## PROJECT CONTEXT\nThe following is project-specific guidance from TARACODE.md:\n\n%s",
			content)
	}

	if content, ok := contextFile(workingDir, "AGENTS.md"); ok {
		prompt += fmt.Sprintf("\n\n## AGENTS.md\nThe following is agent-specific guidance from AGENTS.md:\n\n%s",
			content)
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
