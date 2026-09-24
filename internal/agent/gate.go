package agent

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// executeOne runs the gates and then the tool, and reports the decision to the observer. Every
// outcome, including a refusal, comes back as text so the model learns what happened. ctx reaches
// the dry run and the tool (ruling P3-R47).
func (a *Assistant) executeOne(ctx gocontext.Context, run toolRun) toolOutcome {
	inv, outcome := a.gateAndRun(ctx, run)
	a.observe(run.call, inv, outcome)
	return outcome
}

// gateAndRun is the order of the gates: classify (a classifier that panics is a refusal), exposure
// (a tool the model was not offered does not run), policy (mode, protected targets, deny patterns),
// audit, dry run, permission, edit preview, execute.
func (a *Assistant) gateAndRun(ctx gocontext.Context, run toolRun) (policy.Invocation, toolOutcome) {
	call := run.call
	inv, panicked, err := a.classify(call)
	if err != nil {
		return inv, toolOutcome{
			result: "Error: " + err.Error(), isError: true, rule: "classifier", reason: err.Error(), err: err,
		}
	}
	if panicked {
		a.audit(inv, "deny", "classifier", inv.Reason, false)
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage("Blocked: "+inv.Reason))
		return inv, toolOutcome{result: "Blocked: " + inv.Reason, denied: true, rule: "classifier", reason: inv.Reason}
	}
	if !a.toolRegistry.Exposed(call.Tool, a.mode) {
		if !a.toolRegistry.Exposed(call.Tool, policy.ModeOperate) {
			return inv, a.refuseUnavailable(call.Tool)
		}
		inv = operateOnly(inv)
	}
	if inv.Classification == policy.Mutate {
		if outcome, ok := a.gateMutation(ctx, inv, call); !ok {
			return inv, outcome
		}
	}
	if call.Tool == "edit_file" && inv.Classification == policy.Mutate {
		proceed, message, err := a.handleEditPreview(call.Params)
		if err != nil {
			return inv, toolOutcome{result: message, isError: true, denied: true, rule: "preview", reason: message}
		}
		if !proceed {
			return inv, toolOutcome{result: message, denied: true, rule: "preview", reason: message}
		}
	}
	outcome := a.runTool(ctx, run)
	outcome.rule, outcome.reason = "read", inv.Reason
	if inv.Classification == policy.Mutate {
		outcome.rule = "policy"
	}
	return inv, outcome
}

// classify runs the tool's classifier. A classifier that panics must never take the session down:
// the call then becomes a mutation whose reason carries the panic and whose command is the call's
// summary, and executeOne refuses and audits it.
func (a *Assistant) classify(call *ToolCall) (inv policy.Invocation, panicked bool, err error) {
	defer func() {
		if p := recover(); p != nil {
			inv = policy.Invocation{Tool: call.Tool, Classification: policy.Mutate, WorkingDir: a.workingDir,
				Command: callSummary(call),
				Reason:  fmt.Sprintf("the %s classifier failed (%v), so the call is refused", call.Tool, p)}
			panicked, err = true, nil
		}
	}()
	inv, err = a.toolRegistry.Classify(call.Tool, call.Params, a.workingDir)
	return inv, false, err
}

// maxCallSummary bounds the command an audit record carries for a call the classifier could not read.
const maxCallSummary = 400

// callSummary names a call for the audit log when its classifier could not: the command string of a
// tool that takes one (shell), else the tool name and its arguments as JSON, cut to maxCallSummary.
func callSummary(call *ToolCall) string {
	if command, ok := call.Params["command"].(string); ok && command != "" {
		return truncateSummary(command)
	}
	args, err := json.Marshal(call.Params)
	if err != nil {
		return call.Tool
	}
	return truncateSummary(call.Tool + " " + string(args))
}

func truncateSummary(s string) string {
	if r := []rune(s); len(r) > maxCallSummary {
		return string(r[:maxCallSummary]) + "..."
	}
	return s
}

// refuseUnavailable answers a call to a tool no mode offers in this session: offline hides the tools
// that reach the internet, and a call the model repeats from a resumed session or makes up must not
// run them anyway.
func (a *Assistant) refuseUnavailable(tool string) toolOutcome {
	message := fmt.Sprintf("Tool '%s' is not available in this session: offline is set, "+
		"so the tools that reach the internet are hidden", tool)
	_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(message))
	return toolOutcome{result: message, denied: true, rule: "unavailable", reason: message}
}

// operateOnly marks a call to a tool investigate mode does not offer as a mutation whatever its
// arguments (an edit_file preview included), so the mode check refuses it with its usual message.
func operateOnly(inv policy.Invocation) policy.Invocation {
	if inv.Classification != policy.Mutate {
		inv.Classification = policy.Mutate
		inv.Reason = inv.Tool + " is only offered in operate mode"
	}
	return inv
}

// gateMutation applies the policy, the dry run and the permission store to a mutate invocation and
// writes the audit line. ok is false when the call must not run; the outcome then carries the text.
func (a *Assistant) gateMutation(ctx gocontext.Context, inv policy.Invocation, call *ToolCall) (toolOutcome, bool) {
	verdict := a.pol.Evaluate(a.mode, inv)
	if !verdict.Allow {
		a.audit(inv, "deny", verdict.Rule, verdict.Reason, false)
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(
			fmt.Sprintf("Blocked by policy (%s): %s", verdict.Rule, verdict.Reason)))
		message := "Blocked by policy: " + verdict.Reason
		return toolOutcome{result: message, denied: true, rule: verdict.Rule, reason: verdict.Reason}, false
	}
	if verdict.DryRun != "" {
		out, err := a.toolRegistry.DryRun(ctx, call.Tool, call.Params, a.workingDir)
		if err != nil {
			a.audit(inv, "deny", "dry_run", err.Error(), true)
			message := fmt.Sprintf("Dry run (%s) failed: %v", verdict.DryRun, err)
			return toolOutcome{result: message, isError: true, denied: true, rule: "dry_run", reason: err.Error()}, false
		}
		_, _ = fmt.Fprintf(a.out, "\n%s Dry run (%s):\n%s\n\n", ui.IconInfo, verdict.DryRun, out)
	}
	switch a.permissionFor(call.Tool) {
	case policy.Deny:
		reason := "denied by the saved permission rule"
		a.audit(inv, "deny", "permission", reason, verdict.DryRun != "")
		ui.DisplayPermissionDenied(call.Tool)
		message := fmt.Sprintf("Tool '%s' is denied by the saved permission rule", call.Tool)
		return toolOutcome{result: message, denied: true, rule: "permission", reason: reason}, false
	case policy.Ask:
		choice := a.confirmPermission(inv, call.Params)
		if choice.Remember {
			a.rememberPermission(call.Tool, choice.Allowed)
		}
		if !choice.Allowed {
			reason := "denied at the prompt"
			a.audit(inv, "deny", "user", reason, verdict.DryRun != "")
			message := fmt.Sprintf("Tool '%s' denied by the user", call.Tool)
			return toolOutcome{result: message, denied: true, rule: "user", reason: reason}, false
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
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(fmt.Sprintf(
			"The %s answer for %s is not remembered: no permission store (run /init)", perm, tool)))
		return
	}
	if err := a.permissions.Set(tool, perm); err != nil {
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(fmt.Sprintf(
			"Could not save the %s rule for %s (it applies until you exit): %v", perm, tool, err)))
		return
	}
	ui.DisplayPermissionSaved(tool, string(perm))
}

// audit writes the record for a mutate-classified call before it runs; a missing log or a write
// failure is shown, never hidden, and does not stop the call.
func (a *Assistant) audit(inv policy.Invocation, decision, rule, reason string, dryRun bool) {
	if a.storage == nil {
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(fmt.Sprintf(
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
		_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(fmt.Sprintf("Audit log write failed: %v", err)))
	}
}

// runTool executes the tool with its spinner and applies the output truncation budget. The tool
// runs under ctx, the turn's caller context, and still carries its own timeout.
func (a *Assistant) runTool(ctx gocontext.Context, run toolRun) toolOutcome {
	call := run.call
	if spinner := a.startToolSpinner(run); spinner != nil {
		defer spinner.Stop()
	}
	start := time.Now()
	output, err := a.toolRegistry.Execute(ctx, call.Tool, call.Params, a.workingDir)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return toolOutcome{result: fmt.Sprintf("Error: %v", err), isError: true, durationMs: durationMs, err: err}
	}
	truncated := TruncateToolOutput(output, call.Tool, a.truncationCfg)
	if truncated.WasTruncated {
		a.truncationEvents = append(a.truncationEvents, truncated)
		output = truncated.Output
	}
	return toolOutcome{result: output, durationMs: durationMs}
}
