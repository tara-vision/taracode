package agent

import (
	gocontext "context"
	"fmt"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// executeOne runs the gate and then the tool. Every outcome, including a refusal, comes back as
// text so the model learns what happened. Order: classify, policy (mode, protected targets, deny
// patterns), audit, dry run, permission, edit preview, execute.
func (a *Assistant) executeOne(run toolRun) toolOutcome {
	call := run.call
	inv, err := a.toolRegistry.Classify(call.Tool, call.Params, a.workingDir)
	if err != nil {
		return toolOutcome{result: "Error: " + err.Error(), isError: true}
	}
	if inv.Classification == policy.Mutate {
		if outcome, ok := a.gateMutation(inv, call); !ok {
			return outcome
		}
	}
	if call.Tool == "edit_file" && inv.Classification == policy.Mutate {
		proceed, message, err := a.handleEditPreview(call.Params)
		if err != nil {
			return toolOutcome{result: message, isError: true, denied: true}
		}
		if !proceed {
			return toolOutcome{result: message, denied: true}
		}
	}
	return a.runTool(run)
}

// gateMutation applies the policy, the dry run and the permission store to a mutate invocation and
// writes the audit line. ok is false when the call must not run; the outcome then carries the text.
func (a *Assistant) gateMutation(inv policy.Invocation, call *ToolCall) (toolOutcome, bool) {
	verdict := a.pol.Evaluate(a.mode, inv)
	if !verdict.Allow {
		a.audit(inv, "deny", verdict.Rule, verdict.Reason, false)
		fmt.Println(a.renderer.WarningMessage(fmt.Sprintf("Blocked by policy (%s): %s", verdict.Rule, verdict.Reason)))
		return toolOutcome{result: "Blocked by policy: " + verdict.Reason, denied: true}, false
	}
	if verdict.DryRun != "" {
		out, err := a.toolRegistry.DryRun(gocontext.Background(), call.Tool, call.Params, a.workingDir)
		if err != nil {
			a.audit(inv, "deny", "dry_run", err.Error(), true)
			message := fmt.Sprintf("Dry run (%s) failed: %v", verdict.DryRun, err)
			return toolOutcome{result: message, isError: true, denied: true}, false
		}
		fmt.Printf("\n%s Dry run (%s):\n%s\n\n", ui.IconInfo, verdict.DryRun, out)
	}
	switch a.permissionFor(call.Tool) {
	case policy.Deny:
		a.audit(inv, "deny", "permission", "denied by the saved permission rule", verdict.DryRun != "")
		ui.DisplayPermissionDenied(call.Tool)
		message := fmt.Sprintf("Tool '%s' is denied by the saved permission rule", call.Tool)
		return toolOutcome{result: message, denied: true}, false
	case policy.Ask:
		choice := a.confirmPermission(inv, call.Params)
		if choice.Remember {
			a.rememberPermission(call.Tool, choice.Allowed)
		}
		if !choice.Allowed {
			a.audit(inv, "deny", "user", "denied at the prompt", verdict.DryRun != "")
			return toolOutcome{result: fmt.Sprintf("Tool '%s' denied by the user", call.Tool), denied: true}, false
		}
	}
	a.audit(inv, "allow", verdict.Rule, "", verdict.DryRun != "")
	return toolOutcome{}, true
}

func (a *Assistant) permissionFor(tool string) policy.Permission {
	if a.permissions == nil {
		return policy.Ask
	}
	return a.permissions.For(tool)
}

// rememberPermission saves an "always" answer for the tool. A missing store or a failed save is
// reported, never silent: without a store nothing is remembered, and a store that cannot be
// written keeps the rule for this session only.
func (a *Assistant) rememberPermission(tool string, allowed bool) {
	perm := policy.Deny
	if allowed {
		perm = policy.Allow
	}
	if a.permissions == nil {
		fmt.Println(a.renderer.WarningMessage(fmt.Sprintf(
			"The %s answer for %s is not remembered: no permission store (run /init)", perm, tool)))
		return
	}
	if err := a.permissions.Set(tool, perm); err != nil {
		fmt.Println(a.renderer.WarningMessage(fmt.Sprintf(
			"Could not save the %s rule for %s (it applies until you exit): %v", perm, tool, err)))
		return
	}
	ui.DisplayPermissionSaved(tool, string(perm))
}

// audit writes the record for a mutate-classified call before it runs; a missing log or a write
// failure is shown, never hidden, and does not stop the call.
func (a *Assistant) audit(inv policy.Invocation, decision, rule, reason string, dryRun bool) {
	if a.storage == nil {
		fmt.Println(a.renderer.WarningMessage(fmt.Sprintf(
			"Audit log unavailable (no project storage): the %s decision for %s was not recorded", decision, inv.Tool)))
		return
	}
	sessionID := ""
	if a.session != nil {
		sessionID = a.session.ID
	}
	targets := map[string]string{}
	if inv.Targets.KubeContext != "" {
		targets["kube_context"] = inv.Targets.KubeContext
	}
	if inv.Targets.KubeNamespace != "" {
		targets["kube_namespace"] = inv.Targets.KubeNamespace
	}
	if inv.Targets.CloudAccount != "" {
		targets["cloud_account"] = inv.Targets.CloudAccount
	}
	if len(inv.Targets.Paths) > 0 {
		targets["paths"] = strings.Join(inv.Targets.Paths, ",")
	}
	if len(inv.Targets.Hosts) > 0 {
		targets["hosts"] = strings.Join(inv.Targets.Hosts, ",")
	}
	err := a.storage.AppendAudit(storage.AuditRecord{
		Time: time.Now(), SessionID: sessionID, Mode: string(a.mode), Tool: inv.Tool, Verb: inv.Verb,
		Classification: string(inv.Classification), Command: inv.Command, Targets: targets,
		Decision: decision, Rule: rule, Reason: reason, DryRun: dryRun,
	})
	if err != nil {
		fmt.Println(a.renderer.WarningMessage(fmt.Sprintf("Audit log write failed: %v", err)))
	}
}

// runTool executes the tool with its spinner and applies the output truncation budget. The tool
// gets its own context: every tool carries its own timeout.
func (a *Assistant) runTool(run toolRun) toolOutcome {
	call := run.call
	if spinner := a.startToolSpinner(run); spinner != nil {
		defer spinner.Stop()
	}
	start := time.Now()
	output, err := a.toolRegistry.Execute(gocontext.Background(), call.Tool, call.Params, a.workingDir)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return toolOutcome{result: fmt.Sprintf("Error: %v", err), isError: true, durationMs: durationMs}
	}
	truncated := TruncateToolOutput(output, call.Tool, a.truncationCfg)
	if truncated.WasTruncated {
		a.truncationEvents = append(a.truncationEvents, truncated)
		output = truncated.Output
	}
	return toolOutcome{result: output, durationMs: durationMs}
}
