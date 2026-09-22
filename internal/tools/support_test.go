package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestNamesListsToolsInRegistrationOrder(t *testing.T) {
	r := NewRegistry(Options{})
	r.Register(newTestTool("b", true, policy.Read))
	r.Register(newTestTool("a", true, policy.Read))
	if got := r.Names(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("%v", got)
	}
}

func TestSortedNamesReturnsNamesInSortedOrder(t *testing.T) {
	r := NewRegistry(Options{})
	r.Register(newTestTool("b", true, policy.Read))
	r.Register(newTestTool("a", true, policy.Read))
	if got := r.sortedNames(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("%v", got)
	}
}

func TestRunCommandReturnsOutputAndReportsFailures(t *testing.T) {
	if out, err := runCommand(context.Background(), "", "echo", "hi"); err != nil || out != "hi" {
		t.Fatalf("echo: %q %v", out, err)
	}
	if out, err := runCommand(context.Background(), "", "true"); err != nil || !strings.Contains(out, "completed with no output") {
		t.Fatalf("no output: %q %v", out, err)
	}
	if _, err := runCommand(context.Background(), "", "false"); err == nil || !strings.Contains(err.Error(), "exited with status") {
		t.Errorf("exit status: %v", err)
	}
	if _, err := runCommand(context.Background(), "", "definitely-not-a-real-binary-xyz"); err == nil {
		t.Error("a missing binary must be an error")
	}
}

func TestRunCommandReportsATimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	if _, err := runCommand(ctx, "", "sleep", "1"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: %v", err)
	}
}
