package evals

import (
	"context"
	"errors"
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
	r := NewReplay(s, nil, runDir)
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
	if err == nil || err.Error() != "no recorded data for this call: shell cat /etc/passwd" {
		t.Fatalf("miss: %v", err) // the model-visible text names no task (ruling P3-R58)
	}
	if m := r.Misses(); len(m) != 1 || m[0] != "shell cat /etc/passwd" {
		t.Fatalf("misses %v", m)
	}
	if calls := r.Calls(); len(calls) != 1 || !calls[0].Miss || calls[0].DryRun || !errors.Is(calls[0].Err, ErrNoFixture) {
		t.Fatalf("calls %+v", calls)
	}
}

// TestReplayErrorsNameNoTask pins ruling P3-R58: no error the model sees names the task, whether a
// miss, a corpus defect or a confined path; the replay's own record keeps the miss as ErrNoFixture.
func TestReplayErrorsNameNoTask(t *testing.T) {
	const task = "refuse-operate-prod-context"
	taskDir, runDir := filepath.Join(t.TempDir(), task), t.TempDir()
	scale := map[string]any{"verb": "scale", "resource": "deployment", "name": "checkout", "args": "--replicas=3"}
	get := map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}
	s, _ := LoadFixtures(taskDir)
	if err := s.Save(Signature("kubectl", get), "NAME READY", false); err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fixtures() {
		if err := os.Remove(filepath.Join(s.Dir(), f.File)); err != nil {
			t.Fatal(err)
		}
	}
	r := NewReplay(s, nil, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	ctx := context.Background()
	calls := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"kubectl", scale, "no recorded data for this call: " + Signature("kubectl", scale)},
		{"kubectl", get, "corpus defect: the recorded data for this call cannot be read: " + Signature("kubectl", get)},
		{"read_file", map[string]any{"path": "../outside.txt"}, "outside the eval's working directory"},
	}
	for _, c := range calls {
		_, err := reg.Execute(ctx, c.tool, c.args, runDir)
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), task) {
			t.Errorf("%s %v: %v", c.tool, c.args, err)
		}
	}
	if got := r.Calls(); len(got) != 2 || !errors.Is(got[0].Err, ErrNoFixture) || errors.Is(got[1].Err, ErrNoFixture) {
		t.Fatalf("calls %+v", got)
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
		!strings.Contains(err.Error(), "outside the eval's working directory") {
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

// TestReplayConfinesEveryFileToolIncludingReads covers ruling P3-R24: read_file, list_files and
// search_files must be confined exactly like write_file and edit_file. An empty path for
// list_files/search_files still resolves to the run directory itself and must pass.
func TestReplayConfinesEveryFileToolIncludingReads(t *testing.T) {
	_, reg, runDir := replayRegistry(t)
	ctx := context.Background()
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"read_file", map[string]any{"path": "/etc/hosts"}},
		{"read_file", map[string]any{"path": "../../.."}},
		{"search_files", map[string]any{"path": "/Users", "pattern": "password"}},
		{"list_files", map[string]any{"path": "/etc"}},
	}
	for _, c := range cases {
		if _, err := reg.Execute(ctx, c.tool, c.args, runDir); err == nil ||
			!strings.Contains(err.Error(), "outside the eval's working directory") {
			t.Errorf("%s %v: %v", c.tool, c.args, err)
		}
	}
	if _, err := reg.Execute(ctx, "list_files", map[string]any{}, runDir); err != nil {
		t.Fatalf("list_files with no path must resolve to the run directory: %v", err)
	}
}

// TestReplayConfinementFollowsSymlinksOutOfTheRunDirectory covers ruling P3-R25: a symlink inside
// the run directory that points outside it must not let a write escape a purely textual check.
func TestReplayConfinementFollowsSymlinksOutOfTheRunDirectory(t *testing.T) {
	_, reg, runDir := replayRegistry(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(runDir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ctx := context.Background()
	_, err := reg.Execute(ctx, "write_file", map[string]any{"path": "link/escape.txt", "content": "x"}, runDir)
	if err == nil || !strings.Contains(err.Error(), "outside the eval's working directory") {
		t.Fatalf("write through a symlink: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(elsewhere, "escape.txt")); statErr == nil {
		t.Fatal("the write escaped through the symlink")
	}
}

// TestReplayApplyRefusedAfterErrorPlan covers the first half of ruling P3-R26: a plan fixture
// recorded as an error must not unlock apply, mirroring terraform_tool.go's plan(), which only
// stores plan state when the whole plan pipeline succeeds.
func TestReplayApplyRefusedAfterErrorPlan(t *testing.T) {
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	if err := s.Save("terraform plan dir=.", "terraform exited with status 1\nno credentials", true); err != nil {
		t.Fatal(err)
	}
	r := NewReplay(s, nil, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	ctx := context.Background()
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "plan"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "no credentials") {
		t.Fatalf("plan: %v", err)
	}
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "run terraform plan first") {
		t.Fatalf("apply after an error plan: %v", err)
	}
}

// TestReplayApplyRefusedAfterPlanMiss covers the other half of ruling P3-R26: a plan the recorder
// never captured must not unlock apply either.
func TestReplayApplyRefusedAfterPlanMiss(t *testing.T) {
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	r := NewReplay(s, nil, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	ctx := context.Background()
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "plan"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "no recorded data") {
		t.Fatalf("plan: %v", err)
	}
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "run terraform plan first") {
		t.Fatalf("apply after a plan miss: %v", err)
	}
}

// TestReplayTerraformPlanStateCrossesShellAndToolForms covers the second half of ruling P3-R26: the
// terraform tool and a shell line aliased to it (ruling P3-R11) must share the same plan-state key,
// in both directions, and a shell apply with no plan is refused exactly like the tool's.
func TestReplayTerraformPlanStateCrossesShellAndToolForms(t *testing.T) {
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	for sig, text := range map[string]string{
		"terraform plan dir=.":  "Plan: 1 to add, 0 to change, 0 to destroy.",
		"terraform apply dir=.": "Apply complete! Resources: 1 added.",
	} {
		if err := s.Save(sig, text, false); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()

	t.Run("shell plan unlocks the tool's apply", func(t *testing.T) {
		r := NewReplay(s, nil, runDir)
		reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
		if _, err := reg.Execute(ctx, "shell", map[string]any{"command": "terraform plan"}, runDir); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "apply"}, runDir); err != nil {
			t.Fatalf("apply: %v", err)
		}
	})
	t.Run("the tool's plan unlocks a shell apply", func(t *testing.T) {
		r := NewReplay(s, nil, runDir)
		reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
		if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "plan"}, runDir); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Execute(ctx, "shell", map[string]any{"command": "terraform apply"}, runDir); err != nil {
			t.Fatalf("apply: %v", err)
		}
	})
	t.Run("a shell apply with no plan is refused", func(t *testing.T) {
		r := NewReplay(s, nil, runDir)
		reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
		_, err := reg.Execute(ctx, "shell", map[string]any{"command": "terraform apply"}, runDir)
		if err == nil || !strings.Contains(err.Error(), "run terraform plan first") {
			t.Fatalf("apply: %v", err)
		}
	})
}

// TestReplayReportsACorpusDefectDistinctFromAMiss covers the last part of ruling P3-R28: a signature
// that is indexed but whose fixture file cannot be read is a corpus defect, not a model miss, so the
// runner can tell "the recorder needs to capture this" apart from "the corpus itself is broken."
func TestReplayReportsACorpusDefectDistinctFromAMiss(t *testing.T) {
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	const sig = "kubectl get pod -n shop"
	if err := s.Save(sig, "NAME READY\ncheckout-1 1/1\n", false); err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fixtures() {
		if err := os.Remove(filepath.Join(s.Dir(), f.File)); err != nil {
			t.Fatal(err)
		}
	}
	r := NewReplay(s, nil, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	_, err := reg.Execute(context.Background(), "kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}, runDir)
	if err == nil || !strings.Contains(err.Error(), "corpus defect") {
		t.Fatalf("defect: %v", err)
	}
	if len(r.Misses()) != 0 {
		t.Fatalf("a corpus defect must not count as a miss: %v", r.Misses())
	}
	if d := r.Defects(); len(d) != 1 || d[0] != sig {
		t.Fatalf("defects: %v", d)
	}
}

// TestReplayConfinementRefusesADanglingSymlink covers ruling P3-R38: evalDeepestAncestor must climb
// past a component only when it does not exist yet, never past one os.Lstat finds (a dangling link
// or a loop) but filepath.EvalSymlinks still cannot resolve - otherwise the link's own, perfectly
// real parent (the run directory) resolves fine and the confinement check passes, while the tool's
// own O_CREATE still follows the link and writes wherever it dangles to.
func TestReplayConfinementRefusesADanglingSymlink(t *testing.T) {
	_, reg, runDir := replayRegistry(t)
	sibling := t.TempDir()
	target := filepath.Join(sibling, "x")
	if err := os.Symlink(target, filepath.Join(runDir, "dangling-file")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ctx := context.Background()
	if _, err := reg.Execute(ctx, "write_file", map[string]any{"path": "dangling-file", "content": "x"}, runDir); err == nil {
		t.Fatal("a dangling symlink must be refused, not followed")
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		t.Fatal("the write followed the dangling link and created the sibling file")
	}
}

// TestReplayTerraformPlanStateKeysOnTheQuotedDirNotASharedPrefix covers ruling P3-R38: a dir
// containing whitespace is quoted in the signature (terraformSignature) and unquoted when parsed
// back (terraformSig), so "my infra" and "my other" key as two distinct directories instead of both
// truncating to the shared leading word "my" under a naive strings.Fields split.
func TestReplayTerraformPlanStateKeysOnTheQuotedDirNotASharedPrefix(t *testing.T) {
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	for sig, text := range map[string]string{
		`terraform plan dir="my infra"`:  "Plan: 1 to add.",
		`terraform apply dir="my infra"`: "Apply complete! (my infra)",
		`terraform apply dir="my other"`: "Apply complete! (my other)",
	} {
		if err := s.Save(sig, text, false); err != nil {
			t.Fatal(err)
		}
	}
	r := NewReplay(s, nil, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	ctx := context.Background()
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "plan", "dir": "my infra"}, runDir); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Execute(ctx, "terraform", map[string]any{"command": "apply", "dir": "my other"}, runDir); err == nil ||
		!strings.Contains(err.Error(), "run terraform plan first") {
		t.Fatalf("apply in a different quoted dir must not be unlocked by another dir's plan: %v", err)
	}
	if out, err := reg.Execute(ctx, "terraform", map[string]any{"command": "apply", "dir": "my infra"}, runDir); err != nil ||
		!strings.Contains(out, "my infra") {
		t.Fatalf("apply in the planned quoted dir must succeed: %q %v", out, err)
	}
}

// TestReplayCloudProviderNeverAliasesAnotherTool covers ruling P3-R38: a cloud call with a provider
// outside aws, az and gcloud must key as "cloud <provider> <args>", never as "<provider> <args>",
// so it cannot collide with a real tool's own signature and, through the plan-state gate, unlock an
// apply no real terraform call ever earned.
func TestReplayCloudProviderNeverAliasesAnotherTool(t *testing.T) {
	taskDir, runDir := t.TempDir(), t.TempDir()
	s, _ := LoadFixtures(taskDir)
	if err := s.Save("terraform plan dir=.", "Plan: 1 to add.", false); err != nil {
		t.Fatal(err)
	}
	r := NewReplay(s, nil, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	_, err := reg.Execute(context.Background(), "cloud", map[string]any{"provider": "terraform", "args": "plan dir=."}, runDir)
	if err == nil || !strings.Contains(err.Error(), "no recorded data") {
		t.Fatalf("cloud provider=terraform must not hit the terraform plan fixture: %v", err)
	}
	if m := r.Misses(); len(m) != 1 || m[0] != "cloud terraform plan dir=." {
		t.Fatalf("misses %v", m)
	}
}

func TestReplayRunsAProbeForRealAndCountsNoMiss(t *testing.T) {
	r, reg, runDir := replayRegistry(t)
	out, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "pwd"}, runDir)
	if err != nil || out != runDir {
		t.Fatalf("pwd: %q %v", out, err)
	}
	out, err = reg.Execute(context.Background(), "shell", map[string]any{"command": "cat notes.txt && echo done"}, runDir)
	if err != nil || out != "real file\ndone" { // each command's output on its own line, as the shell tool shows them
		t.Fatalf("a chain of probes: %q %v", out, err)
	}
	if m := r.Misses(); len(m) != 0 {
		t.Fatalf("misses %v", m)
	}
	calls := r.Calls()
	if len(calls) != 2 || !calls[0].Real || !calls[1].Real || calls[0].Signature != "shell pwd" {
		t.Fatalf("calls %+v", calls)
	}
	if reals := r.RealCalls(); len(reals) != 2 || reals[1] != "shell cat notes.txt && echo done" {
		t.Fatalf("real calls %v", reals)
	}
}

func TestReplayAProbeOffTheListStaysAMiss(t *testing.T) {
	r, reg, runDir := replayRegistry(t)
	for _, line := range []string{"uname -a", "cat /etc/hosts"} { // the registry rebuilds the error as text
		if _, err := reg.Execute(context.Background(), "shell", map[string]any{"command": line}, runDir); err == nil ||
			!strings.HasPrefix(err.Error(), "no recorded data for this call: shell "+line) {
			t.Fatalf("%s: %v", line, err)
		}
	}
	if m := r.Misses(); len(m) != 2 || len(r.RealCalls()) != 0 {
		t.Fatalf("misses %v, real %v", m, r.RealCalls())
	}
	for _, c := range r.Calls() {
		if !c.Miss || c.Real || !errors.Is(c.Err, ErrNoFixture) {
			t.Fatalf("call %+v", c)
		}
	}
}

func TestReplayADryRunShellProbeReplays(t *testing.T) {
	r, _, runDir := replayRegistry(t)
	exec := r.Middleware(tools.Call{Tool: "shell", DryRun: true}, nil)
	if _, err := exec(context.Background(), map[string]any{"command": "pwd"}, runDir); err == nil || !errors.Is(err, ErrNoFixture) {
		t.Fatalf("a dry run never probes: %v", err)
	}
	if m := r.Misses(); len(m) != 1 || m[0] != "dryrun:shell pwd" {
		t.Fatalf("misses %v", m)
	}
}

// replayRegistryWithCommon is replayRegistry with a common store holding the given fixtures.
func replayRegistryWithCommon(t *testing.T, common map[string]string) (*Replay, *tools.Registry, string) {
	t.Helper()
	cs, err := LoadFixtures(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for sig, text := range common {
		if err := cs.Save(sig, text, false); err != nil {
			t.Fatal(err)
		}
	}
	r, _, runDir := replayRegistry(t)
	r = NewReplay(r.store, cs, runDir)
	reg := tools.NewBuiltinRegistry(tools.Options{Middleware: r.Middleware}, tools.Config{})
	return r, reg, runDir
}

func TestReplayFallsBackToTheCommonStore(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{"shell which terraform": "/usr/local/bin/terraform\n"})
	out, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which terraform"}, runDir)
	if err != nil || out != "/usr/local/bin/terraform\n" {
		t.Fatalf("which: %q %v", out, err)
	}
	if calls := r.Calls(); len(calls) != 1 || !calls[0].Common || calls[0].Miss || calls[0].Real {
		t.Fatalf("calls %+v", calls)
	}
	if m := r.Misses(); len(m) != 0 {
		t.Fatalf("misses %v", m)
	}
}

func TestATaskFixtureWinsOverTheCommonStore(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{"shell which terraform": "/usr/local/bin/terraform\n"})
	if err := r.store.Save("shell which terraform", "/opt/bin/terraform\n", false); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which terraform"}, runDir)
	if err != nil || out != "/opt/bin/terraform\n" {
		t.Fatalf("which: %q %v", out, err)
	}
	if calls := r.Calls(); len(calls) != 1 || calls[0].Common {
		t.Fatalf("calls %+v", calls)
	}
}

func TestReplayAnswersAChainOfFixtureParts(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{ // fixtures as the recorder writes them: trimmed
		"shell which terraform": "/usr/local/bin/terraform",
		// a shell line that is one terraform command keys like the terraform tool, at record time as here
		Signature("shell", map[string]any{"command": "terraform version"}): "Terraform v1.9.0\non darwin_arm64",
		"shell which helm": "\n[exit status 1]",
	})
	out, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which terraform && terraform version"}, runDir)
	if err != nil || out != "/usr/local/bin/terraform\nTerraform v1.9.0\non darwin_arm64" {
		t.Fatalf("chain: %q %v", out, err)
	}
	if calls := r.Calls(); len(calls) != 1 || !calls[0].Common || calls[0].Signature != "shell which terraform && terraform version" {
		t.Fatalf("calls %+v", calls)
	}
	out, err = reg.Execute(context.Background(), "shell", map[string]any{"command": "pwd && which terraform"}, runDir)
	if err != nil || out != runDir+"\n/usr/local/bin/terraform" {
		t.Fatalf("a chain of a probe and a fixture: %q %v", out, err)
	}
	if calls := r.Calls(); len(calls) != 2 || !calls[1].Common || !calls[1].Real {
		t.Fatalf("calls %+v", calls)
	}
	out, err = reg.Execute(context.Background(), "shell", map[string]any{"command": "which helm && terraform version"}, runDir)
	if err != nil || out != "\n[exit status 1]" { // the shell tool's shape for a failure without output
		t.Fatalf("a recorded failure stops the chain at &&: %q %v", out, err)
	}
}

func TestAChainWithAMissingPartIsAMiss(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{"shell which terraform": "/usr/local/bin/terraform\n"})
	_, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which terraform && terraform -version"}, runDir)
	if err == nil || err.Error() != "no recorded data for this call: shell which terraform && terraform -version" {
		t.Fatalf("miss: %v", err)
	}
	if m := r.Misses(); len(m) != 1 || m[0] != "shell which terraform && terraform -version" {
		t.Fatalf("misses %v", m)
	}
}

func TestAChainNeverReadsATasksOwnStore(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{"shell which helm": "/usr/local/bin/helm"})
	if err := r.store.Save("shell helm version", "version.BuildInfo{Version:\"v3.19.0\"}", false); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which helm && helm version"}, runDir); err == nil ||
		!strings.HasPrefix(err.Error(), "no recorded data for this call") {
		t.Fatalf("a part only the task's store holds keeps the line a miss, so the scorer and the plan gate see whole lines: %v", err)
	}
	if err := r.store.Save("shell which helm && helm version", "/usr/local/bin/helm\nversion.BuildInfo{Version:\"v3.19.0\"}", false); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which helm && helm version"}, runDir)
	if err != nil || !strings.HasPrefix(out, "/usr/local/bin/helm") {
		t.Fatalf("a recorded whole line is served as recorded: %q %v", out, err)
	}
	if calls := r.Calls(); len(calls) != 2 || calls[1].Common || calls[1].Real {
		t.Fatalf("calls %+v", calls)
	}
}

func TestReplayServesACommonErrorFixture(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{"shell which terraform": "/usr/local/bin/terraform"})
	if err := r.common.Save("shell which helm", "which: refused by the sandbox", true); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Execute(context.Background(), "shell", map[string]any{"command": "which helm"}, runDir)
	if err == nil || !strings.Contains(err.Error(), "refused by the sandbox") {
		t.Fatalf("err %v", err)
	}
}

func TestReplayReportsACommonFixtureItCannotRead(t *testing.T) {
	r, reg, runDir := replayRegistryWithCommon(t, map[string]string{"shell which helm": "/usr/local/bin/helm"})
	entries, err := os.ReadDir(r.common.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries { // the index stays, the fixture file goes: a defect of the common store
		if e.Name() != "index.yaml" {
			if err := os.Remove(filepath.Join(r.common.Dir(), e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err = reg.Execute(context.Background(), "shell", map[string]any{"command": "which helm"}, runDir)
	if err == nil || !strings.HasPrefix(err.Error(), "corpus defect: ") {
		t.Fatalf("err %v", err)
	}
	if calls := r.Calls(); len(calls) != 1 || !calls[0].Defect || !calls[0].Common {
		t.Fatalf("calls %+v", calls)
	}
}
