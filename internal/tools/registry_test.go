package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

func newTestTool(name string, readForm bool, class policy.Classification) *Tool {
	return &Tool{
		Name: name, Description: "test " + name, ReadForm: readForm,
		Params: []Param{{Name: "x", Type: "string", Description: "x", Required: true}},
		Classify: func(_ map[string]any, _ string) policy.Invocation {
			return policy.Invocation{Tool: name, Classification: class, Reason: "fixed"}
		},
		Run: func(_ context.Context, args map[string]any, _ string) (string, error) {
			return "ran " + name + " x=" + argString(args, "x") + " secret=AKIAIOSFODNN7EXAMPLE", nil
		},
	}
}

func TestRegistryExposesReadFormToolsInInvestigateMode(t *testing.T) {
	r := NewRegistry(Options{})
	r.Register(newTestTool("reader", true, policy.Read))
	r.Register(newTestTool("writer", false, policy.Mutate))
	r.Register(&Tool{Name: "ext", Description: "external", ReadForm: true, External: true,
		Classify: func(map[string]any, string) policy.Invocation {
			return policy.Invocation{Tool: "ext", Classification: policy.Read}
		},
		Run: func(context.Context, map[string]any, string) (string, error) { return "", nil }})
	names := func(defs []openaiTool) []string {
		var out []string
		for _, d := range defs {
			out = append(out, d.Function.Name)
		}
		return out
	}
	if got := names(r.Definitions(policy.ModeInvestigate)); strings.Join(got, ",") != "reader,ext" {
		t.Errorf("investigate %v", got)
	}
	if got := names(r.Definitions(policy.ModeOperate)); strings.Join(got, ",") != "reader,writer,ext" {
		t.Errorf("operate %v", got)
	}
	if r.Available(policy.ModeInvestigate) != 2 || r.Available(policy.ModeOperate) != 3 {
		t.Errorf("available %d %d", r.Available(policy.ModeInvestigate), r.Available(policy.ModeOperate))
	}
	offline := NewRegistry(Options{Offline: true})
	offline.Register(newTestTool("reader", true, policy.Read))
	offline.Register(&Tool{Name: "ext", Description: "external", ReadForm: true, External: true,
		Classify: func(map[string]any, string) policy.Invocation { return policy.Invocation{} },
		Run:      func(context.Context, map[string]any, string) (string, error) { return "", nil }})
	if got := names(offline.Definitions(policy.ModeOperate)); strings.Join(got, ",") != "reader" {
		t.Errorf("offline %v", got)
	}
	def := r.Definitions(policy.ModeOperate)[0]
	raw, _ := json.Marshal(def)
	if !strings.Contains(string(raw), `"required":["x"]`) || !strings.Contains(string(raw), `"type":"object"`) {
		t.Errorf("schema %s", raw)
	}
}

func TestRegistryExecuteRedactsAndClassifies(t *testing.T) {
	red, _ := redact.New(redact.Options{})
	r := NewRegistry(Options{Redactor: red})
	r.Register(newTestTool("reader", true, policy.Read))
	out, err := r.Execute(context.Background(), "reader", map[string]any{"x": "1"}, t.TempDir())
	if err != nil || out != "ran reader x=1 secret=[redacted:aws-access-key]" {
		t.Fatalf("%q %v", out, err)
	}
	if r.Redactions() != 1 {
		t.Errorf("redactions %d", r.Redactions())
	}
	if _, err := r.Execute(context.Background(), "missing", nil, ""); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("unknown tool: %v", err)
	}
	inv, err := r.Classify("reader", map[string]any{"x": "1"}, "/w")
	if err != nil || inv.Classification != policy.Read || inv.WorkingDir != "/w" {
		t.Errorf("%+v %v", inv, err)
	}
	if _, err := r.DryRun(context.Background(), "reader", nil, ""); err != ErrNoDryRun {
		t.Errorf("dry run: %v", err)
	}
}

func TestRegistryMCPToolsAreGroupedByServer(t *testing.T) {
	r := NewRegistry(Options{})
	r.RegisterMCP(newTestTool("gh.list", true, policy.Read), "gh")
	r.RegisterMCP(newTestTool("gh.create", false, policy.Mutate), "gh")
	if !r.IsMCP("gh.list") || len(r.MCPTools()["gh"]) != 2 {
		t.Fatalf("%v", r.MCPTools())
	}
	if r.Available(policy.ModeInvestigate) != 1 {
		t.Errorf("mutate MCP tools must be hidden in investigate mode")
	}
	r.UnregisterMCP("gh")
	if _, ok := r.Get("gh.list"); ok || len(r.MCPTools()) != 0 {
		t.Error("unregister")
	}
}

func TestRegisterPanicsOnDuplicateNames(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	r := NewRegistry(Options{})
	r.Register(newTestTool("a", true, policy.Read))
	r.Register(newTestTool("a", true, policy.Read))
}

func TestExecuteAndSetHistoryDoNotRace(t *testing.T) {
	r := NewRegistry(Options{})
	r.Register(newTestTool("reader", true, policy.Read))
	hm, err := history.NewManager(t.TempDir(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if _, err := r.Execute(context.Background(), "reader", map[string]any{"x": "1"}, dir); err != nil {
				t.Error(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			r.SetHistory(hm)
		}
	}()
	wg.Wait()
}

func TestRegistryDryRunRedactsErrorsAndKeepsErrNoDryRun(t *testing.T) {
	red, _ := redact.New(redact.Options{})
	r := NewRegistry(Options{Redactor: red})
	failing := newTestTool("failing", false, policy.Mutate)
	failing.DryRun = func(context.Context, map[string]any, string) (string, error) {
		return "out AKIAIOSFODNN7EXAMPLE", errors.New("failing exited with status 2\ntoken AKIAIOSFODNN7EXAMPLE rejected")
	}
	none := newTestTool("none", false, policy.Mutate)
	none.DryRun = func(context.Context, map[string]any, string) (string, error) { return "", ErrNoDryRun }
	r.Register(failing)
	r.Register(none)

	out, err := r.DryRun(context.Background(), "failing", nil, "")
	if err == nil || strings.Contains(err.Error(), "AKIAIOSFODNN7EXAMPLE") ||
		!strings.Contains(err.Error(), "exited with status 2\ntoken [redacted:aws-access-key] rejected") {
		t.Fatalf("the dry run error must be redacted: %v", err)
	}
	if out != "out [redacted:aws-access-key]" {
		t.Fatalf("the dry run output must be redacted: %q", out)
	}
	if _, err := r.DryRun(context.Background(), "none", nil, ""); !errors.Is(err, ErrNoDryRun) {
		t.Fatalf("a tool's ErrNoDryRun must survive the redaction: %v", err)
	}
}
