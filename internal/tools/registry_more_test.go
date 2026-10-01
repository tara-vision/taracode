package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestUnregisterMCPKeepsTheOtherTools(t *testing.T) {
	r := NewRegistry(Options{})
	r.Register(newTestTool("builtin", true, policy.Read))
	r.RegisterMCP(newTestTool("gh_issues", true, policy.Read), "github")
	r.RegisterMCP(newTestTool("db_query", true, policy.Read), "postgres")
	r.RegisterMCP(newTestTool("gh_prs", true, policy.Read), "github")
	r.UnregisterMCP("github")
	var names []string
	for _, def := range r.Definitions(policy.ModeOperate) {
		names = append(names, def.Function.Name)
	}
	if strings.Join(names, ",") != "builtin,db_query" || r.IsMCP("gh_prs") || !r.IsMCP("db_query") {
		t.Fatalf("left %v", names)
	}
}

func TestRegistryWithoutTheTool(t *testing.T) {
	r := NewRegistry(Options{})
	if r.Redactions() != 0 {
		t.Fatal("no redactor, no redactions")
	}
	if _, err := r.Classify("kubectl", map[string]any{}, ""); err == nil || err.Error() != `unknown tool "kubectl"` {
		t.Fatalf("Classify: %v", err)
	}
	if _, err := r.DryRun(context.Background(), "kubectl", map[string]any{}, ""); err == nil || err.Error() != `unknown tool "kubectl"` {
		t.Fatalf("DryRun: %v", err)
	}
	if err := r.ArgumentError("kubectl", map[string]any{"args": "-f x.yaml"}); err != nil {
		t.Fatalf("a check for a tool the registry does not hold: %v", err)
	}
}
