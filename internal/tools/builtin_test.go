package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestBuiltinListAndSchemaBudget(t *testing.T) {
	r := NewBuiltinRegistry(Options{}, Config{})
	want := "read_file,list_files,search_files,write_file,edit_file,shell,git,kubectl,helm,terraform,docker,cloud,scan,web_search,web_fetch"
	if got := strings.Join(r.Names(), ","); got != want {
		t.Fatalf("tools %s", got)
	}
	raw, err := json.Marshal(r.Definitions(policy.ModeOperate))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 8192 {
		t.Fatalf("the 15 schemas are %d bytes; the budget is 8192 (spec 5.3)", len(raw))
	}
	if n := r.Available(policy.ModeInvestigate); n != 13 {
		t.Errorf("investigate mode exposes %d tools, want 13", n)
	}
	offline := NewBuiltinRegistry(Options{Offline: true}, Config{})
	if n := offline.Available(policy.ModeInvestigate); n != 11 {
		t.Errorf("offline investigate mode exposes %d tools, want 11", n)
	}
	for _, name := range r.Names() {
		tool, _ := r.Get(name)
		if tool.Description == "" || tool.Classify == nil || tool.Run == nil {
			t.Errorf("%s is incomplete", name)
		}
		for _, p := range tool.Params {
			//nolint:staticcheck // the three-way type check reads clearer than the De Morgan rewrite
			if p.Description == "" || !(p.Type == "string" || p.Type == "integer" || p.Type == "boolean") {
				t.Errorf("%s.%s has no description or a bad type", name, p.Name)
			}
		}
	}
}
