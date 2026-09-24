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

func TestResolvePlaceholdersWalksNestedValuesAndRejectsLeftoverPlaceholders(t *testing.T) {
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
	// Only what is left of an eval placeholder fails (ruling P3-R68); any other brace pair is the
	// command's own template and passes through untouched.
	args, err = resolvePlaceholders(context.Background(), map[string]any{"x": "{{unknown}} stays"}, resolve)
	if err != nil || args["x"] != "{{unknown}} stays" {
		t.Fatalf("a brace pair that is not an eval placeholder must pass through untouched: %v %v", args, err)
	}
	nestedLeftovers := map[string]any{
		"list":   []any{"fine", "{{nodes}}"},
		"nested": map[string]any{"inner": "{{pods app=a}}"},
	}
	for key, value := range nestedLeftovers {
		if _, err := resolvePlaceholders(context.Background(), map[string]any{key: value}, resolve); err == nil {
			t.Fatalf("a misspelled placeholder nested in %s must fail, not reach a recorded call", key)
		}
	}
}

// TestResolvePlaceholdersLeavesGoTemplatesUntouched covers ruling P3-R68: a docker --format or a
// kubectl -o go-template is a Go template, not an eval placeholder, and must reach the recorded call
// exactly as written, whether or not a resolver is given; the resolver is never asked about it.
func TestResolvePlaceholdersLeavesGoTemplatesUntouched(t *testing.T) {
	cases := []struct{ key, value string }{
		// The two docker inspect formats the container-crashed task records, verbatim.
		{"args", "inspect crashed-app --format Cmd={{json .Config.Cmd}} Entrypoint={{json .Config.Entrypoint}} " +
			"Binds={{json .HostConfig.Binds}}"},
		{"args", "inspect crashed-app --format ExitCode={{.State.ExitCode}} OOM={{.State.OOMKilled}} " +
			"Error={{.State.Error}} RestartCount={{.RestartCount}}"},
		// The first one as a shell line, the form the ruling quotes.
		{"command", "docker inspect crashed-app --format Cmd={{json .Config.Cmd}} " +
			"Entrypoint={{json .Config.Entrypoint}} Binds={{json .HostConfig.Binds}}"},
		// A kubectl go-template that even mentions a node, after the dot of a field.
		{"args", `-o go-template={{range .items}}{{ .spec.nodeName }}{{"\n"}}{{end}}`},
	}
	for _, tc := range cases {
		for _, resolve := range []Resolver{nil, func(_ context.Context, kind, spec string) (string, error) {
			t.Errorf("the resolver must not be asked about a Go template: %s %s", kind, spec)
			return "", nil
		}} {
			args, err := resolvePlaceholders(context.Background(), map[string]any{tc.key: tc.value}, resolve)
			if err != nil || args[tc.key] != tc.value {
				t.Fatalf("%q must resolve unchanged: %v %v", tc.value, args, err)
			}
		}
	}
	// A real placeholder next to a Go template: the placeholder is resolved, the template is kept.
	resolve := func(_ context.Context, kind, spec string) (string, error) { return kind + "(" + spec + ")", nil }
	args, err := resolvePlaceholders(context.Background(),
		map[string]any{"command": "docker inspect {{pod app=a}} --format {{json .State}}"}, resolve)
	if err != nil || args["command"] != "docker inspect pod(app=a) --format {{json .State}}" {
		t.Fatalf("%v %v", args, err)
	}
}

// TestResolvePlaceholdersRejectsALeftoverEvalPlaceholder covers the other half of ruling P3-R68:
// "{{" with optional spaces before pod, pods, node or nodes is what a misspelled, unterminated or
// unresolvable eval placeholder leaves behind, and it must still fail rather than reach a recorded
// call.
func TestResolvePlaceholdersRejectsALeftoverEvalPlaceholder(t *testing.T) {
	resolve := func(_ context.Context, kind, spec string) (string, error) { return kind + "(" + spec + ")", nil }
	cases := []struct {
		name, value string
		resolve     Resolver
	}{
		{"misspelled plural", "{{pods app=x}}", resolve},
		{"no resolver", "{{ node }}", nil},
		{"misspelled plural, spaced", "logs {{ nodes }} --tail=5", resolve},
		{"unterminated", "logs {{ pod app=x --tail=5", resolve},
	}
	for _, tc := range cases {
		if args, err := resolvePlaceholders(context.Background(), map[string]any{"x": tc.value}, tc.resolve); err == nil {
			t.Fatalf("%s: %q must fail, got %v", tc.name, tc.value, args)
		}
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
