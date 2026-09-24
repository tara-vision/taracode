package evals

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestPrivateNamesEnvVarReplacesDefaults(t *testing.T) {
	t.Setenv("TARACODE_EVALS_PRIVATE_NAMES", " kiosk-7 , , warehouse-42 ")
	names, err := privateNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "kiosk-7" || names[1] != "warehouse-42" {
		t.Fatalf("names=%v", names)
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

func TestResolvePlaceholders(t *testing.T) {
	resolve := func(_ context.Context, kind, spec string) (string, error) {
		return kind + "(" + spec + ")", nil
	}
	args, err := resolvePlaceholders(context.Background(), map[string]any{
		"name": "{{pod app=checkout ns=shop}}", "node": "{{ node }}", "n": 3, "plain": "x"}, resolve)
	if err != nil || args["name"] != "pod(app=checkout ns=shop)" || args["node"] != "node()" || args["n"] != 3 || args["plain"] != "x" {
		t.Fatalf("%v %v", args, err)
	}
	if _, err := resolvePlaceholders(context.Background(), map[string]any{"x": "{{pod app=a}}"},
		func(context.Context, string, string) (string, error) { return "", errors.New("nothing matched") }); err == nil {
		t.Fatal("resolver errors must surface")
	}
}

func TestResolvePlaceholdersWalksNestedValuesAndRejectsLeftoverBraces(t *testing.T) {
	resolve := func(_ context.Context, kind, spec string) (string, error) {
		return kind + "(" + spec + ")", nil
	}
	args, err := resolvePlaceholders(context.Background(), map[string]any{
		"list":   []any{"{{pod app=a}}", 5},
		"nested": map[string]any{"inner": "{{node}}"},
	}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	list, ok := args["list"].([]any)
	if !ok || list[0] != "pod(app=a)" || list[1] != 5 {
		t.Fatalf("list=%v", args["list"])
	}
	nested, ok := args["nested"].(map[string]any)
	if !ok || nested["inner"] != "node()" {
		t.Fatalf("nested=%v", args["nested"])
	}
	if _, err := resolvePlaceholders(context.Background(), map[string]any{"x": "{{unknown}} stays"}, resolve); err == nil {
		t.Fatal("a brace pair the pattern does not recognize must fail, not reach a recorded call")
	}
}

func TestRecordTaskRunsSetupCallsAndTeardown(t *testing.T) {
	t.Setenv("TARACODE_EVALS_PRIVATE_NAMES", "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "demo")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := "#!/bin/sh\nprintf 'from setup' > \"$EVAL_WORKDIR/marker.txt\"\n"
	teardown := "#!/bin/sh\nprintf 'ran' > \"$EVAL_SCENARIO/teardown-ran\"\n"
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte(setup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte(teardown), 0o755); err != nil {
		t.Fatal(err)
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/demo",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"cat marker.txt\"}}\n    - {tool: shell, args: {command: \"cat missing.txt\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	if err := os.MkdirAll(filepath.Join(dir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workdir", "readme.txt"), []byte("copied"), 0o644); err != nil {
		t.Fatal(err)
	}
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := RecordTask(context.Background(), task, scenarios, nil, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	s, _ := LoadFixtures(dir)
	if out, isErr, ok := s.Lookup("shell cat marker.txt"); !ok || isErr || out != "from setup" {
		t.Fatalf("fixture %q %v %v\n%s", out, isErr, ok, log.String())
	}
	if out, isErr, ok := s.Lookup("shell cat missing.txt"); !ok || isErr || !strings.Contains(out, "[exit status 1]") {
		t.Fatalf("the failing command must be recorded as its output with the exit status (the shell tool returns no error): %q %v %v\n%s", out, isErr, ok, log.String())
	}
	if _, err := os.Stat(filepath.Join(scenario, "teardown-ran")); err != nil {
		t.Fatal("teardown did not run")
	}
	if !strings.Contains(log.String(), "2 fixtures") {
		t.Fatalf("log %q", log.String())
	}
}

func TestRecordTaskAbortsOnHostnameRefusal(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	// Pinned explicitly (ruling P3-R41 item 4): if the sandbox this runs in already exports
	// TARACODE_EVALS_PRIVATE_NAMES, relying on defaultPrivateNames would deny list something other
	// than this real hostname, and the refusal this test expects would never trigger.
	t.Setenv(privateNamesEnvVar, hostname)
	t.Setenv("TARACODE_TEST_HOSTNAME_ECHO", hostname)
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "leaky")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := "#!/bin/sh\nexit 0\n"
	teardown := "#!/bin/sh\nprintf 'ran' > \"$EVAL_SCENARIO/teardown-ran\"\n"
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte(setup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte(teardown), 0o755); err != nil {
		t.Fatal(err)
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/leaky",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: 'printf \"%s\" \"$TARACODE_TEST_HOSTNAME_ECHO\"'}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	err = RecordTask(context.Background(), task, scenarios, nil, &log)
	if !errors.Is(err, ErrFixtureHostname) {
		t.Fatalf("expected an ErrFixtureHostname, got %v\n%s", err, log.String())
	}
	if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
		t.Fatal("teardown did not run")
	}
	s, _ := LoadFixtures(dir)
	if s.Len() != 0 {
		t.Fatalf("no fixture must be saved when the recorder refuses the host name, got %d", s.Len())
	}
}

func TestRecordTaskAbortsOnContextCancellationMidRun(t *testing.T) {
	t.Setenv("TARACODE_EVALS_PRIVATE_NAMES", "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "cut-short")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/cut-short",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"echo first\"}}\n    - {tool: shell, args: {command: \"echo {{node}}\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	resolve := func(context.Context, string, string) (string, error) {
		cancel() // simulates the caller giving up partway through the run
		return "second", nil
	}
	var log strings.Builder
	err = RecordTask(ctx, task, scenarios, resolve, &log)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context.Canceled error, got %v\n%s", err, log.String())
	}
	if !strings.Contains(log.String(), "ok             shell echo first") {
		t.Fatalf("the call before the cancellation must still have run\n%s", log.String())
	}
	if !strings.Contains(log.String(), "cancelled") || !strings.Contains(log.String(), "shell echo second") {
		t.Fatalf("the aborting call must be named in the log (ruling P3-R41 item 6)\n%s", log.String())
	}
	// recordCalls records into a fresh store and only swaps it into fixtures/ once every call
	// succeeds (ruling P3-R36): an abort must never mix that call's fixture with the cancelled
	// call's absence, so the real fixtures stay exactly as they were before this run (here, absent).
	s, _ := LoadFixtures(dir)
	if s.Len() != 0 {
		t.Fatalf("an aborted run must not touch the real fixtures at all, got %d\n%s", s.Len(), log.String())
	}
	if _, _, ok := s.Lookup("shell echo second"); ok {
		t.Fatal("the cancelled call must not be recorded")
	}
}

func TestRecordTaskRejectsASymlinkInWorkdirBeforeSetupRuns(t *testing.T) {
	t.Setenv("TARACODE_EVALS_PRIVATE_NAMES", "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "linked")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := "#!/bin/sh\nprintf 'setup ran' > \"$EVAL_SCENARIO/setup-ran\"\n"
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte(setup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/linked",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"true\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	workdir := filepath.Join(dir, "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(workdir, "escape")); err != nil {
		t.Fatal(err)
	}
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := RecordTask(context.Background(), task, scenarios, nil, &log); err == nil {
		t.Fatalf("expected an error for the symlink\n%s", log.String())
	}
	if _, err := os.Stat(filepath.Join(scenario, "setup-ran")); err == nil {
		t.Fatal("setup must not have run")
	}
}

func TestRecordTaskRunsTeardownWhenSetupFails(t *testing.T) {
	t.Setenv("TARACODE_EVALS_PRIVATE_NAMES", "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "broken")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := "#!/bin/sh\nexit 1\n"
	teardown := "#!/bin/sh\nprintf 'ran' > \"$EVAL_SCENARIO/teardown-ran\"\n"
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte(setup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte(teardown), 0o755); err != nil {
		t.Fatal(err)
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/broken",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"true\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := RecordTask(context.Background(), task, scenarios, nil, &log); err == nil {
		t.Fatal("expected the setup failure to surface")
	}
	if _, err := os.Stat(filepath.Join(scenario, "teardown-ran")); err != nil {
		t.Fatal("teardown did not run after a setup failure")
	}
}

func TestRecordTaskReRecordingReplacesStaleFixtures(t *testing.T) {
	t.Setenv("TARACODE_EVALS_PRIVATE_NAMES", "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "reused")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	firstYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/reused",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"echo first\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", firstYAML)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := RecordTask(context.Background(), task, scenarios, nil, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	s, _ := LoadFixtures(dir)
	if _, _, ok := s.Lookup("shell echo first"); !ok {
		t.Fatalf("first recording missing its fixture\n%s", log.String())
	}

	secondYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/reused",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"echo second\"}}").Replace(goodTask)
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte(secondYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	task2, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Reset()
	if err := RecordTask(context.Background(), task2, scenarios, nil, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	s2, _ := LoadFixtures(dir)
	if s2.Len() != 1 {
		t.Fatalf("stale fixtures survived a re-record: %d entries", s2.Len())
	}
	if _, _, ok := s2.Lookup("shell echo second"); !ok {
		t.Fatalf("second recording missing its fixture\n%s", log.String())
	}
	if _, _, ok := s2.Lookup("shell echo first"); ok {
		t.Fatal("the first recording's fixture must not survive a re-record")
	}
}

func TestNamesFromHostAddsTheLabelBeforeTheFirstDot(t *testing.T) {
	if got := namesFromHost("mymac.local"); len(got) != 2 || got[0] != "mymac.local" || got[1] != "mymac" {
		t.Fatalf("%v", got)
	}
	if got := namesFromHost("bare"); len(got) != 1 || got[0] != "bare" {
		t.Fatalf("%v", got)
	}
}

func TestParseResolvConfTrimsTrailingDots(t *testing.T) {
	got := parseResolvConf("search example.com. corp.internal\ndomain example.net.\n")
	if len(got) != 3 || got[0] != "example.com" || got[1] != "corp.internal" || got[2] != "example.net" {
		t.Fatalf("%v", got)
	}
}

func TestSwapFixturesRollsBackWhenTheSecondRenameFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions")
	}
	dir := t.TempDir()
	finalDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// newDir's own parent, not finalDir's, is locked: renaming finalDir aside touches only finalDir's
	// parent and must still succeed, while renaming newDir in requires removing its entry from this
	// now-read-only parent and fails, exercising exactly the rollback path.
	newParent := filepath.Join(dir, "locked")
	if err := os.MkdirAll(newParent, 0o755); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(newParent, "fixtures")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(newParent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(newParent, 0o755) }) // so t.TempDir()'s own cleanup can remove it

	if err := swapFixtures(finalDir, newDir); err == nil {
		t.Fatal("expected the second rename to fail")
	}
	data, err := os.ReadFile(filepath.Join(finalDir, "old.txt"))
	if err != nil || string(data) != "old" {
		t.Fatalf("the previous fixtures must be restored: %v %q", err, data)
	}
	if _, err := os.Stat(finalDir + ".replaced"); err == nil {
		t.Fatal("the aside copy must not remain after a successful rollback")
	}
}

func TestRecordTaskToleratesABackgroundedSetupProcess(t *testing.T) {
	t.Setenv(privateNamesEnvVar, "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "backgrounded")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	setup := "#!/bin/sh\nsleep 30 &\necho $! > \"$EVAL_SCENARIO/bg.pid\"\n"
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte(setup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/backgrounded",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: shell, args: {command: \"true\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := RecordTask(context.Background(), task, scenarios, nil, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if !strings.Contains(log.String(), "background process running") {
		t.Fatalf("expected a warning about the background process, not a failure\n%s", log.String())
	}
	pidBytes, err := os.ReadFile(filepath.Join(scenario, "bg.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(pid, 0); errors.Is(killErr, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the backgrounded sleep (pid %d) is still running after RecordTask returned", pid)
}

func TestRecordTaskSavesADryRunWithNoDryRunAsAnErrorFixture(t *testing.T) {
	t.Setenv(privateNamesEnvVar, "not-a-real-name")
	root := t.TempDir()
	scenarios := filepath.Join(root, "scenarios")
	scenario := filepath.Join(scenarios, "kubernetes", "no-dry-run")
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenario, "teardown.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// kubectl has a DryRun function that itself refuses any verb but apply (so this goes through the
	// middleware, which already saves it); shell has no DryRun at all (Registry.DryRun returns
	// ErrNoDryRun before the middleware ever runs, so recordOneCall must save it directly). Both must
	// end up as error fixtures, logged the same way.
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/no-dry-run",
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}",
		"- {tool: kubectl, dry_run: true, args: {verb: get, resource: pods, namespace: shop}}\n"+
			"    - {tool: shell, dry_run: true, args: {command: \"true\"}}").Replace(goodTask)
	dir := writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	if err := RecordTask(context.Background(), task, scenarios, nil, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	s, _ := LoadFixtures(dir)
	kubectlArgs := map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}
	if out, isErr, ok := s.Lookup(DryRunSignature("kubectl", kubectlArgs)); !ok || !isErr || out != "this tool has no dry run" {
		t.Fatalf("kubectl (has DryRun, refuses this verb) fixture %q %v %v\n%s", out, isErr, ok, log.String())
	}
	shellArgs := map[string]any{"command": "true"}
	if out, isErr, ok := s.Lookup(DryRunSignature("shell", shellArgs)); !ok || !isErr || out != "this tool has no dry run" {
		t.Fatalf("shell (no DryRun at all) fixture %q %v %v\n%s", out, isErr, ok, log.String())
	}
	if strings.Count(log.String(), "error recorded") < 2 {
		t.Fatalf("expected both no-dry-run calls logged as \"error recorded\"\n%s", log.String())
	}
	if strings.Contains(log.String(), "skipped") {
		t.Fatalf("no call should be logged as skipped\n%s", log.String())
	}
}
