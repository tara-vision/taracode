package evals

import (
	"context"
	"errors"
	"testing"
)

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
