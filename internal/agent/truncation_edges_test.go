package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
)

func TestTruncationHelpersLeaveShortInputAlone(t *testing.T) {
	lines := []string{"a", "b"}
	if got := truncateAtBoundary(lines, 5, "x"); len(got) != 2 {
		t.Fatalf("truncateAtBoundary() = %v", got)
	}
	if got := truncateJSONBoundary(lines, 5); len(got) != 2 {
		t.Fatalf("truncateJSONBoundary() = %v", got)
	}
	if got := truncateChars("short", 10); got != "short" {
		t.Fatalf("truncateChars() = %q", got)
	}
}

// TestJSONIsCutAtAClosingLine: past the line limit, JSON output ends at the last closing brace or
// bracket in the final quarter of the kept lines.
func TestJSONIsCutAtAClosingLine(t *testing.T) {
	lines := []string{"[", "  {", "    a", "  },", "  {", "    b", "  },", "  {", "    c", "    c2", "  },", "  {", "    d",
		"  }", "]"}
	got := truncateAtBoundary(lines, 12, "kubectl")
	if strings.Join(got, "\n") != strings.Join(lines[:11], "\n") {
		t.Fatalf("kept %q", got)
	}
	if got := truncateJSONBoundary([]string{"{", "1", "2", "3", "4", "5", "6", "7", "8", "9"}, 8); len(got) != 8 {
		t.Fatalf("no closing line near the limit keeps the limit: %d", len(got))
	}
}

func TestBinaryOutputIsDetectedAndShortened(t *testing.T) {
	binary := strings.Repeat("a", 600) + "\x00" + strings.Repeat("b", 400)
	if isBinaryContent(binary) {
		t.Fatal("only the first 512 bytes are inspected")
	}
	early := "\x00" + strings.Repeat("b", 400)
	res := TruncateBinaryOutput(early, "read_file", 0)
	if !res.WasTruncated || res.KeptChars != 256 || !strings.HasPrefix(res.Output, "[Binary content detected, showing first 256 bytes]\n") {
		t.Fatalf("%+v", res)
	}
}

func TestWindowWarningsCombine(t *testing.T) {
	window, warning := ResolveContextWindow("lots", 8192)
	if window != 8192 || warning != `context.window "lots" is not a number; using auto; context window 8192 is below 16384; `+
		`tool-heavy sessions will compact early` {
		t.Fatalf("%d %q", window, warning)
	}
	a, _ := newTestAssistant(t, false)
	var out bytes.Buffer
	a.out = &out
	a.configuredWindow = "auto"
	a.resolveAndApplyWindow(8192)
	if a.contextWindow != 8192 || a.compactionCfg.MaxTokens != 8192 || !strings.Contains(out.String(), "is below 16384") {
		t.Fatalf("window %d, budget %d, output %q", a.contextWindow, a.compactionCfg.MaxTokens, out.String())
	}
}

func TestSystemPromptCarriesTheProjectMemories(t *testing.T) {
	dir := t.TempDir()
	mm, err := memory.NewManager(filepath.Join(dir, ".taracode"))
	if err != nil {
		t.Fatal(err)
	}
	mem, err := mm.Create(storage.MemoryCategoryDecision, "Deploy with Helm", "", nil, storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildSystemPrompt(dir, nil, policy.ModeInvestigate, 2000)
	if !strings.Contains(prompt, "## PROJECT MEMORIES") || !strings.Contains(prompt, "- [decision] Deploy with Helm\n") {
		t.Fatalf("%s", prompt)
	}
	reloaded, err := memory.NewManager(filepath.Join(dir, ".taracode"))
	if err != nil {
		t.Fatal(err)
	}
	for _, meta := range reloaded.List() {
		if meta.ID == mem.ID && meta.UseCount != 1 {
			t.Fatalf("putting a memory in the prompt counts a use: %+v", meta)
		}
	}
	if strings.Contains(buildSystemPrompt(dir, nil, policy.ModeInvestigate, 0), "PROJECT MEMORIES") {
		t.Fatal("a zero budget leaves the memories out")
	}
}

func TestGetMemoryManagerNeedsAUsableDirectory(t *testing.T) {
	dir := t.TempDir()
	if getMemoryManager(dir) != nil {
		t.Fatal("no .taracode, no memories")
	}
	if err := os.WriteFile(filepath.Join(dir, ".taracode"), []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if getMemoryManager(dir) != nil {
		t.Fatal(".taracode that is not a directory")
	}
}

func TestServerContextCheckBackOffs(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	a.llm = nil
	a.checkServerContextOnce()
	if a.serverContextChecked {
		t.Fatal("no client, nothing checked")
	}

	b, srv := newTestAssistant(t, false)
	srv.PsStatus = 500
	b.checkServerContextOnce()
	if b.serverContextChecked || b.serverContextTokens != 0 {
		t.Fatal("a failed check is tried again later")
	}

	vllm := ollamatest.New(t)
	c := newForTest(t.TempDir(), "gemma4:12b", vllm.URL, false)
	c.llm = provider.NewVLLMProvider(vllm.URL, "").LLM()
	c.checkServerContextOnce()
	if !c.serverContextChecked {
		t.Fatal("a backend that cannot report its context is not asked again")
	}
}
