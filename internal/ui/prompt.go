package ui

import (
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/tara-vision/taracode/internal/policy"
)

// PermissionChoice is the answer to one permission prompt: whether this invocation may run and
// whether to remember the answer for the tool.
type PermissionChoice struct {
	Allowed  bool
	Remember bool
}

// PromptPermission asks whether one mutate invocation may run and whether to remember the answer
// for this tool.
func PromptPermission(inv policy.Invocation, args map[string]any) PermissionChoice {
	fmt.Println()
	fmt.Printf("%s %s wants to run a mutation\n", IconWarning, Bold.Render(inv.Tool))
	if inv.Command != "" {
		fmt.Printf("   Command: %s\n", inv.Command)
	} else if summary := formatParams(args); summary != "" {
		fmt.Printf("   Params: %s\n", Subtle.Render(summary))
	}
	if inv.Reason != "" {
		fmt.Printf("   Why: %s\n", Subtle.Render(inv.Reason))
	}
	if t := inv.Targets; t.KubeContext != "" || t.KubeNamespace != "" || t.CloudAccount != "" {
		fmt.Printf("   Target: context=%s namespace=%s account=%s\n", t.KubeContext, t.KubeNamespace, t.CloudAccount)
	}
	fmt.Println()
	choices := []string{
		"Yes, this time", "No, this time",
		"Always allow " + inv.Tool + " mutations", "Always deny " + inv.Tool + " mutations",
	}
	prompt := promptui.Select{Label: "Allow?", Items: choices, Size: 4,
		Templates: &promptui.SelectTemplates{Label: "{{ . }}", Active: "\U0001F449 {{ . | cyan }}", Inactive: "   {{ . }}",
			Selected: "\U00002705 {{ . | green }}"}}
	idx, _, err := prompt.Run()
	if err != nil {
		return PermissionChoice{}
	}
	return PermissionChoice{Allowed: idx == 0 || idx == 2, Remember: idx >= 2}
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

// DisplayPermissionSaved shows confirmation when a remembered answer is saved for a tool
func DisplayPermissionSaved(tool, perm string) {
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("%s Saved: %s → %s", IconSuccess, tool, perm)))
}
