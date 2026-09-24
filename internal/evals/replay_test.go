package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/tools"
)

func replayRegistry(t *testing.T) (*Replay, *tools.Registry, string) {
	t.Helper()
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	for sig, text := range map[string]string{
		"kubectl get pod -n shop":         "NAME       READY   STATUS             RESTARTS\ncheckout-1 0/1     CrashLoopBackOff   5",
		"dryrun:helm upgrade web ./chart": "NAME: web\nSTATUS: pending-upgrade (dry run)",
		"terraform plan dir=.":            "Plan: 1 to add, 0 to change, 0 to destroy.",
		"dryrun:terraform apply dir=.":    "Plan from 4:05PM:\nPlan: 1 to add",
		"terraform apply dir=.":           "Apply complete! Resources: 1 added.",
	} {
		if err := s.Save(sig, text, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save("kubectl logs checkout-1 -n shop", "kubectl exited with status 1\nno previous logs", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "notes.txt"), []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewReplay(s, "crashloop-oomkilled", runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	return r, reg, runDir
}

func TestReplayServesFixturesForToolsAndShellAlike(t *testing.T) {
	r, reg, runDir := replayRegistry(t)
	ctx := context.Background()
	out, err := reg.Execute(ctx, "kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}, runDir)
	if err != nil || !strings.Contains(out, "CrashLoopBackOff") {
		t.Fatalf("kubectl: %q %v", out, err)
	}
	out, err = reg.Execute(ctx, "shell", map[string]any{"command": "kubectl get pods -n shop"}, runDir)
	if err != nil || !strings.Contains(out, "CrashLoopBackOff") {
		t.Fatalf("shell alias: %q %v", out, err)
	}
	if _, err = reg.Execute(ctx, "kubectl", map[string]any{"verb": "logs", "name": "checkout-1", "namespace": "shop"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "no previous logs") {
		t.Fatalf("error fixture: %v", err)
	}
	if len(r.Misses()) != 0 {
		t.Fatalf("misses %v", r.Misses())
	}
}

func TestReplayMissIsAnErrorAndCounted(t *testing.T) {
	r, reg, runDir := replayRegistry(t)
	_, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "cat /etc/passwd"}, runDir)
	if err == nil || !strings.Contains(err.Error(), "no recorded data for this call in eval task crashloop-oomkilled") {
		t.Fatalf("miss: %v", err)
	}
	if m := r.Misses(); len(m) != 1 || m[0] != "shell cat /etc/passwd" {
		t.Fatalf("misses %v", m)
	}
	if calls := r.Calls(); len(calls) != 1 || !calls[0].Miss || calls[0].DryRun {
		t.Fatalf("calls %+v", calls)
	}
}

func TestReplayRunsFileToolsForRealAndConfinesWrites(t *testing.T) {
	_, reg, runDir := replayRegistry(t)
	ctx := context.Background()
	out, err := reg.Execute(ctx, "read_file", map[string]any{"path": "notes.txt"}, runDir)
	if err != nil || out != "real file" {
		t.Fatalf("read_file: %q %v", out, err)
	}
	if _, err := reg.Execute(ctx, "write_file", map[string]any{"path": "../escape.txt", "content": "x"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "outside the task directory") {
		t.Fatalf("escape: %v", err)
	}
	if _, err := reg.Execute(ctx, "write_file", map[string]any{"path": filepath.Join(os.TempDir(), "taracode-escape.txt"),
		"content": "x"}, runDir); err == nil {
		t.Fatal("absolute escape allowed")
	}
	if _, err := reg.Execute(ctx, "write_file", map[string]any{"path": "sub/new.txt", "content": "ok"}, runDir); err != nil {
		t.Fatalf("inside: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(runDir, "sub", "new.txt")); string(data) != "ok" {
		t.Fatal("the confined write did not happen")
	}
}

func TestReplayDryRunsAndTerraformPlanState(t *testing.T) {
	r, reg, runDir := replayRegistry(t)
	ctx := context.Background()
	out, err := reg.DryRun(ctx, "helm", map[string]any{"args": "upgrade web ./chart"}, runDir)
	if err != nil || !strings.Contains(out, "dry run") {
		t.Fatalf("helm dry run: %q %v", out, err)
	}
	if _, err := reg.DryRun(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "run terraform plan first") {
		t.Fatalf("apply before plan: %v", err)
	}
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "plan"}, runDir); err != nil {
		t.Fatal(err)
	}
	if out, err := reg.DryRun(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err != nil || !strings.Contains(out, "Plan from") {
		t.Fatalf("apply dry run after plan: %q %v", out, err)
	}
	if out, err := reg.Execute(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err != nil || !strings.Contains(out, "Apply complete") {
		t.Fatalf("apply: %q %v", out, err)
	}
	if _, err := reg.DryRun(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err == nil {
		t.Fatal("the plan should be consumed by apply")
	}
	if len(r.Misses()) != 0 {
		t.Fatalf("misses %v", r.Misses())
	}
}
