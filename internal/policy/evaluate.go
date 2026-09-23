package policy

import (
	"fmt"
	"strings"
)

// Verdict is the policy's answer for one invocation.
type Verdict struct {
	Allow  bool
	Rule   string // read | mode | protected.<list> | deny.commands | policy
	Reason string // set on every denial, phrased for the model and the user
	DryRun string // kubectl_apply | terraform_apply | helm_upgrade when a dry run must come first
}

// Evaluate applies, in order: classification (reads always pass), mode, protected targets, deny
// patterns, dry-run requirements. Permissions are the caller's next step.
func (p Policy) Evaluate(mode Mode, inv Invocation) Verdict {
	if inv.Classification != Mutate {
		return Verdict{Allow: true, Rule: "read"}
	}
	if mode != ModeOperate {
		return Verdict{Rule: "mode", Reason: fmt.Sprintf(
			"investigate mode is read-only; %s was classified as a mutation (%s). Ask the user to switch with /mode operate",
			describe(inv), inv.Reason)}
	}
	if v, denied := p.protectedTarget(inv); denied {
		return v
	}
	if inv.Command != "" {
		for _, pattern := range p.Deny.Commands {
			if matchGlob(pattern, collapse(inv.Command)) {
				return Verdict{Rule: "deny.commands", Reason: fmt.Sprintf("%q matches the deny pattern %q", inv.Command, pattern)}
			}
		}
	}
	return Verdict{Allow: true, Rule: "policy", DryRun: p.dryRunFor(inv)}
}

func (p Policy) protectedTarget(inv Invocation) (Verdict, bool) {
	t := inv.Targets
	if pat, ok := firstGlob(p.Protected.KubeContexts, t.KubeContext); ok {
		return denied("protected.kube_contexts", "kube context %q matches the protected pattern %q", t.KubeContext, pat), true
	}
	if t.KubeNamespace == "*" && len(p.Protected.KubeNamespaces) > 0 {
		return denied("protected.kube_namespaces", "the command touches every namespace (-A), including the protected %s",
			strings.Join(p.Protected.KubeNamespaces, ", ")), true
	}
	if pat, ok := firstGlob(p.Protected.KubeNamespaces, t.KubeNamespace); ok {
		return denied("protected.kube_namespaces",
			"namespace %q matches the protected pattern %q", t.KubeNamespace, pat), true
	}
	for _, candidate := range append([]string{t.CloudAccount}, strings.Fields(inv.Command)...) {
		if pat, ok := firstGlob(p.Protected.CloudAccounts, candidate); ok {
			return denied("protected.cloud_accounts", "%q matches the protected cloud account pattern %q", candidate, pat), true
		}
	}
	for _, path := range t.Paths {
		for _, pat := range p.Protected.Paths {
			if matchPath(pat, path, inv.WorkingDir) {
				return denied("protected.paths", "%s matches the protected path pattern %q", path, pat), true
			}
		}
	}
	for _, candidate := range append(append([]string{}, t.Hosts...), strings.Fields(inv.Command)...) {
		if pat, ok := firstGlob(p.Protected.Hosts, candidate); ok {
			return denied("protected.hosts", "%q matches the protected host pattern %q", candidate, pat), true
		}
	}
	return Verdict{}, false
}

func denied(rule, format string, args ...any) Verdict {
	return Verdict{Rule: rule, Reason: fmt.Sprintf(format, args...)}
}

func firstGlob(patterns []string, value string) (string, bool) {
	if value == "" {
		return "", false
	}
	for _, pat := range patterns {
		if matchGlob(pat, value) {
			return pat, true
		}
	}
	return "", false
}

func (p Policy) dryRunFor(inv Invocation) string {
	switch {
	case inv.Tool == "kubectl" && inv.Verb == "apply" && enabled(p.RequireDryRun.KubectlApply):
		return "kubectl_apply"
	case inv.Tool == "terraform" && inv.Verb == "apply" && enabled(p.RequireDryRun.TerraformApply):
		return "terraform_apply"
	case inv.Tool == "helm" && (inv.Verb == "upgrade" || inv.Verb == "install") && enabled(p.RequireDryRun.HelmUpgrade):
		return "helm_upgrade"
	}
	return ""
}

// describe names an invocation for messages: "shell `rm -rf build`" or "write_file /w/x".
func describe(inv Invocation) string {
	if inv.Command != "" {
		return fmt.Sprintf("%s `%s`", inv.Tool, inv.Command)
	}
	if len(inv.Targets.Paths) > 0 {
		return inv.Tool + " " + strings.Join(inv.Targets.Paths, ", ")
	}
	return inv.Tool
}
