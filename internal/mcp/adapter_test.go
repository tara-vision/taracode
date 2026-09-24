package mcp

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestToToolClassifiesByTheCallersDecision(t *testing.T) {
	tool := MCPTool{Name: "github.get_issue", ServerName: "github", OriginalName: "get_issue", ReadOnly: true}
	read := ToTool(nil, tool, true)
	if !read.ReadForm || read.Classify(nil, "").Classification != policy.Read || read.Classify(nil, "").Reason != "" {
		t.Fatalf("read tool %+v", read.Classify(nil, ""))
	}
	mut := ToTool(nil, tool, false)
	inv := mut.Classify(nil, "")
	if mut.ReadForm || inv.Classification != policy.Mutate || !strings.Contains(inv.Reason, "policy") {
		t.Fatalf("mutate tool %+v", inv)
	}
}
