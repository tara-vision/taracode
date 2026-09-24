package evals

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

func fakeTool(name, output string, fail bool) *tools.Tool {
	return &tools.Tool{Name: name, Description: name, ReadForm: true,
		Params: []tools.Param{{Name: "args", Type: "string", Description: "args"}},
		Classify: func(map[string]any, string) policy.Invocation {
			return policy.Invocation{Tool: name, Classification: policy.Read}
		},
		Run: func(context.Context, map[string]any, string) (string, error) {
			if fail {
				return "", errors.New(output)
			}
			return output, nil
		}}
}

func TestRecorderRedactsBeforeSavingAndRecordsErrors(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	red, _ := redact.New(redact.Options{})
	rec := NewRecorder(s, red, nil)
	reg := tools.NewRegistry(tools.Options{Redactor: red, Middleware: rec.Middleware})
	reg.Register(fakeTool("helm", "NAME: web\npassword=hunter2hunter2\n", false))
	reg.Register(fakeTool("git", "git exited with status 128\nnot a repository", true))
	if _, err := reg.Execute(context.Background(), "helm", map[string]any{"args": "status web"}, taskDir); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Execute(context.Background(), "git", map[string]any{"args": "status"}, taskDir); err == nil {
		t.Fatal("the recorder must pass the tool's error through")
	}
	out, isErr, ok := s.Lookup("helm status web")
	if !ok || isErr || strings.Contains(out, "hunter2hunter2") || !strings.Contains(out, "[redacted:") {
		t.Fatalf("saved %q %v %v", out, isErr, ok)
	}
	if out, isErr, ok := s.Lookup("git status"); !ok || !isErr || !strings.Contains(out, "not a repository") {
		t.Fatalf("error fixture %q %v %v", out, isErr, ok)
	}
	if saved := rec.Saved(); len(saved) != 2 {
		t.Fatalf("saved %v", saved)
	}
}

func TestRecorderRefusesAPrivateNameInOutputText(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	rec := NewRecorder(s, nil, []string{"kiosk-7"})
	reg := tools.NewRegistry(tools.Options{Middleware: rec.Middleware})
	reg.Register(fakeTool("kubectl", "server: https://kiosk-7:6443", false))
	_, err := reg.Execute(context.Background(), "kubectl", map[string]any{"args": "x"}, taskDir)
	if err == nil || !errors.Is(rec.Refused(), ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("err=%v refused=%v len=%d", err, rec.Refused(), s.Len())
	}
}

func TestRecorderRefusesAPrivateNameInItsSignature(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	rec := NewRecorder(s, nil, []string{"kiosk-7"})
	reg := tools.NewRegistry(tools.Options{Middleware: rec.Middleware})
	reg.Register(fakeTool("shell", "ok", false))
	_, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "true kiosk-7"}, taskDir)
	if err == nil || !errors.Is(rec.Refused(), ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("err=%v refused=%v len=%d", err, rec.Refused(), s.Len())
	}
	// Sticky: a second, otherwise clean call refuses too, without ever running the tool. Its own
	// error, like the first call's, does not survive the registry (see Refused's doc), so the check
	// is the same: err merely non-nil, and rec.Refused() still the sentinel.
	reg.Register(fakeTool("git", "clean output", false))
	if _, err := reg.Execute(context.Background(), "git", map[string]any{"args": "status"}, taskDir); err == nil {
		t.Fatal("a refusal must stay sticky: expected an error")
	}
	if !errors.Is(rec.Refused(), ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("no fixture must be saved after a refusal: refused=%v len=%d", rec.Refused(), s.Len())
	}
}

func TestRecorderDoesNotRefuseALongerWordContainingTheName(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	rec := NewRecorder(s, nil, []string{"lab"})
	reg := tools.NewRegistry(tools.Options{Middleware: rec.Middleware})
	reg.Register(fakeTool("kubectl", "labels: {}", false))
	if _, err := reg.Execute(context.Background(), "kubectl", map[string]any{"args": "x"}, taskDir); err != nil {
		t.Fatal(err)
	}
	if rec.Refused() != nil || s.Len() != 1 {
		t.Fatalf("refused=%v len=%d", rec.Refused(), s.Len())
	}
}

func TestRecorderMatchesPrivateNamesCaseInsensitively(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	rec := NewRecorder(s, nil, []string{"Kiosk-7"})
	reg := tools.NewRegistry(tools.Options{Middleware: rec.Middleware})
	reg.Register(fakeTool("kubectl", "server: https://KIOSK-7:6443", false))
	_, err := reg.Execute(context.Background(), "kubectl", map[string]any{"args": "x"}, taskDir)
	if err == nil || !errors.Is(rec.Refused(), ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("err=%v refused=%v len=%d", err, rec.Refused(), s.Len())
	}
}

func TestRecorderSavesADryRunUnderDryRunSignature(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	rec := NewRecorder(s, nil, nil)
	reg := tools.NewRegistry(tools.Options{Middleware: rec.Middleware})
	reg.Register(&tools.Tool{Name: "terraform", Description: "terraform", ReadForm: true,
		Params: []tools.Param{{Name: "args", Type: "string", Description: "args"}},
		Classify: func(map[string]any, string) policy.Invocation {
			return policy.Invocation{Tool: "terraform", Classification: policy.Read}
		},
		Run: func(context.Context, map[string]any, string) (string, error) { return "applied", nil },
		DryRun: func(context.Context, map[string]any, string) (string, error) {
			return "Plan: 1 to add, 0 to change, 0 to destroy", nil
		},
	})
	args := map[string]any{"args": "apply"}
	out, err := reg.DryRun(context.Background(), "terraform", args, taskDir)
	if err != nil || out != "Plan: 1 to add, 0 to change, 0 to destroy" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	got, isErr, ok := s.Lookup(DryRunSignature("terraform", args))
	if !ok || isErr || got != "Plan: 1 to add, 0 to change, 0 to destroy" {
		t.Fatalf("dry run fixture %q %v %v", got, isErr, ok)
	}
	if _, _, ok := s.Lookup(Signature("terraform", args)); ok {
		t.Fatal("a dry run must not be saved under the non-dry-run signature")
	}
}

// TestRecorderGuardAndSaveGuardsADirectSaveLikeTheMiddleware pins the guards the RecordTask tests
// cannot reach on the direct path (fix round 4), calling guardAndSave the way recordOneCall does for a
// dry run of a tool with no DryRun: a call whose context is already done saves nothing, a private
// name in the text is refused and latched, and the refusal stays sticky for a later, clean save.
func TestRecorderGuardAndSaveGuardsADirectSaveLikeTheMiddleware(t *testing.T) {
	s, _ := LoadFixtures(t.TempDir())
	rec := NewRecorder(s, nil, []string{"kiosk-7"})
	done, cancel := context.WithCancel(context.Background())
	cancel()
	kept, err := rec.guardAndSave(done, "dryrun:shell true", "this tool has no dry run", true, keepExisting)
	if kept || err != nil || s.Len() != 0 {
		t.Fatalf("a call whose context is done must save nothing: kept=%v err=%v len=%d", kept, err, s.Len())
	}
	_, err = rec.guardAndSave(context.Background(), "dryrun:shell true", "reached kiosk-7", true, keepExisting)
	if !errors.Is(err, ErrFixtureHostname) || !errors.Is(rec.Refused(), ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("a private name in the text must be refused and latched: err=%v refused=%v len=%d",
			err, rec.Refused(), s.Len())
	}
	_, err = rec.guardAndSave(context.Background(), "dryrun:shell echo clean", "clean", true, keepExisting)
	if !errors.Is(err, ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("the refusal must stay sticky for a later, clean save: err=%v len=%d", err, s.Len())
	}
}
