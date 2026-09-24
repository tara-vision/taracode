package evals

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	rec := NewRecorder(s, red, "")
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

func TestRecorderRefusesTheHostname(t *testing.T) {
	taskDir := t.TempDir()
	s, _ := LoadFixtures(taskDir)
	rec := NewRecorder(s, nil, "lab-vm-7")
	reg := tools.NewRegistry(tools.Options{Middleware: rec.Middleware})
	reg.Register(fakeTool("kubectl", "server: https://lab-vm-7:6443", false))
	_, err := reg.Execute(context.Background(), "kubectl", map[string]any{"args": "x"}, taskDir)
	if err == nil || !errors.Is(rec.Refused(), ErrFixtureHostname) || s.Len() != 0 {
		t.Fatalf("err=%v refused=%v len=%d", err, rec.Refused(), s.Len())
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

func TestRecordTaskRunsSetupCallsAndTeardown(t *testing.T) {
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
		"- {tool: shell, args: {command: \"printf '%s' "+hostname+"\"}}").Replace(goodTask)
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
