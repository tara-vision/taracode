package evals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taraengine/pkg/api"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

const testSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEngineInfoCarriesOriginAndSource(t *testing.T) {
	srv := fakeEngine(t)
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000}}
	srv.Models[0].Origin = "import"
	srv.Models[0].Source = &api.Source{Repo: "unsloth/Qwen3-GGUF", File: "sub/q4.gguf", Path: "/Users/dejan/models/q4.gguf",
		SHA256: testSHA}
	info, _, err := engineInfo(context.Background(), runOptions(srv, ""))
	if err != nil || info.Origin != "import" || info.Source == nil {
		t.Fatalf("%+v %v", info, err)
	}
	if s := *info.Source; s.Repo != "unsloth/Qwen3-GGUF" || s.File != "sub/q4.gguf" || s.Path != "q4.gguf" || s.SHA256 != testSHA {
		t.Fatalf("the source as recorded (a local path keeps its base name only): %+v", s)
	}
	srv.Models[0].Origin = "catalog"
	srv.Models[0].Source = nil
	if info, _, err := engineInfo(context.Background(), runOptions(srv, "")); err != nil || info.Origin != "catalog" ||
		info.Source != nil {
		t.Fatalf("a catalog model: %+v %v", info, err)
	}
}

func TestEngineInfoCleansTheSource(t *testing.T) {
	srv := fakeEngine(t)
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000}}
	srv.Models[0].Origin = "import" // a local import as the engine links it: the GGUF's name, up to 64 characters
	srv.Models[0].Source = &api.Source{Path: "/data/models/local/Qwen3.6-35B-A3B-Instruct-UD-Q4_K_XL:latest.gguf", SHA256: testSHA}
	if info, _, err := engineInfo(context.Background(), runOptions(srv, "")); err != nil || info.Source == nil ||
		info.Source.Path != "Qwen3.6-35B-A3B-Instruct-UD-Q4_K_XL:latest.gguf" {
		t.Fatalf("a long local name keeps its base name: %+v %v", info, err)
	}
	srv.Models[0].Source = &api.Source{Repo: "../x", File: "q4.gguf"}
	if info, _, err := engineInfo(context.Background(), runOptions(srv, "")); err != nil || info.Source == nil ||
		info.Source.Repo != "" || info.Source.File != "q4.gguf" {
		t.Fatalf("a repository id of dot segments is dropped: %+v %v", info, err)
	}
	srv.Models[0].Origin = "somewhere | http://10.1.2.3"
	srv.Models[0].Source = &api.Source{Repo: "not a repo", File: "../x.gguf", Path: "/", SHA256: "sha256:nope"}
	if info, _, err := engineInfo(context.Background(), runOptions(srv, "")); err != nil || info.Origin != "" ||
		info.Source != nil {
		t.Fatalf("odd engine strings are dropped, not published: %+v %v", info, err)
	}
}

// outsideResult is a hardware result from a model with the given origin, override and source.
func outsideResult(model, origin, override string, source *EngineSource) Results {
	r := hardwareResult(model, "48", 0.9, 0.9, 300, testEngine(30_000_000_000, 30_000_000_000, 100))
	r.Engine.Origin, r.Engine.Source, r.Override = origin, source, override
	return r
}

const bolt = "docs/evals/overrides/nemotron-3.5-bolt-74b.registry.yaml"

func unrankedOf(t *testing.T, sb Scoreboard, model string) string {
	t.Helper()
	for _, b := range sb.Boards {
		for _, r := range b.Rows {
			if r.Model == model {
				t.Fatalf("%s is ranked on %s", model, b.Hardware)
			}
		}
		for _, r := range b.Unpublished {
			if r.Model == model {
				return r.Unranked
			}
		}
	}
	for _, tier := range sb.Tiers {
		if slicesContains(boardModels(tier.Rows), model) {
			t.Fatalf("%s sits in the %s tier table", model, tier.Tier)
		}
	}
	t.Fatalf("%s is nowhere on the boards", model)
	return ""
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestARegistryRowWithoutAPublishedOverrideIsNotRanked(t *testing.T) {
	results := append(hardwareResults(), outsideResult("nemotron-3.5-bolt:74b", "registry", "", nil))
	sb := testBoard(t, results, testCorpus)
	if got := unrankedOf(t, sb, "nemotron-3.5-bolt:74b"); got != "unpublished override" {
		t.Fatalf("unranked %q", got)
	}
	md := sb.Markdown()
	if !strings.Contains(md, "\nNot ranked:\n\n- nemotron-3.5-bolt:74b on ollama 0.35.0: from an unpublished override\n") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestAnOverrideRowIsMarkedInMarkdownAndJSON(t *testing.T) {
	results := append(hardwareResults(), outsideResult("nemotron-3.5-bolt:74b", "registry", bolt, nil))
	sb := testBoard(t, results, testCorpus)
	var row Row
	for _, r := range sb.Boards[0].Rows {
		if r.Model == "nemotron-3.5-bolt:74b" {
			row = r
		}
	}
	if row.Override != bolt || row.Origin != "registry" || row.Unranked != "" {
		t.Fatalf("row %+v", row)
	}
	md := sb.Markdown()
	if !strings.Contains(md, "| nemotron-3.5-bolt:74b [1] |") || !strings.Contains(md, "\n[1] outside the catalog: "+bolt+"\n") {
		t.Fatalf("markdown:\n%s", md)
	}
	if n := strings.Count(md, "[1] outside the catalog: "+bolt); n != 2 { // once under the board, once under its tier table
		t.Fatalf("the footnote appears %d times:\n%s", n, md)
	}
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	boards := back["boards"].([]any)
	rows := boards[0].(map[string]any)["rows"].([]any)
	found := false
	for _, r := range rows {
		if m := r.(map[string]any); m["model"] == "nemotron-3.5-bolt:74b" {
			found = m["override"] == bolt && m["origin"] == "registry"
		}
	}
	if !found {
		t.Fatalf("the JSON row carries the override:\n%s", data)
	}
}

func TestAnImportRowRanksWithItsFileAndIsMarked(t *testing.T) {
	src := &EngineSource{Repo: "unsloth/Qwen3-GGUF", File: "q4.gguf", SHA256: testSHA}
	results := append(hardwareResults(), outsideResult("qwen-import", "import", "", src))
	sb := testBoard(t, results, testCorpus)
	md := sb.Markdown()
	if !strings.Contains(md, "| qwen-import [1] |") ||
		!strings.Contains(md, "\n[1] outside the catalog: unsloth/Qwen3-GGUF/q4.gguf, sha256 0123456789ab\n") {
		t.Fatalf("markdown:\n%s", md)
	}
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back Scoreboard
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	var found *EngineSource
	for _, r := range back.Boards[0].Rows {
		if r.Model == "qwen-import" {
			found = r.Source
		}
	}
	if found == nil || *found != *src || !strings.Contains(string(data), `"source": {`) {
		t.Fatalf("the JSON row carries the source: %+v\n%s", found, data)
	}
	local := &EngineSource{Path: "local.gguf", SHA256: testSHA}
	sb = testBoard(t, append(hardwareResults(), outsideResult("local-import", "import", "", local)), testCorpus)
	if md := sb.Markdown(); !strings.Contains(md, "\n[1] outside the catalog: local.gguf, sha256 0123456789ab\n") {
		t.Fatalf("a local import names its file:\n%s", md)
	}
}

func TestAnImportRowWithoutItsFileIsNotRanked(t *testing.T) {
	sb := testBoard(t, append(hardwareResults(), outsideResult("qwen-import", "import", "", nil)), testCorpus)
	if got := unrankedOf(t, sb, "qwen-import"); got != "import without its file" {
		t.Fatalf("unranked %q", got)
	}
	shaOnly := &EngineSource{SHA256: testSHA} // the engine named a file the results could not keep
	sb = testBoard(t, append(hardwareResults(), outsideResult("sha-import", "import", "", shaOnly)), testCorpus)
	if got := unrankedOf(t, sb, "sha-import"); got != "import without its file" {
		t.Fatalf("a source with only a sha256: %q", got)
	}
	if md := sb.Markdown(); !strings.Contains(md, "- sha-import on ollama 0.35.0: an import without its file\n") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestARowWithoutAnOriginIsNotRanked(t *testing.T) {
	results := append(hardwareResults(), outsideResult("old-engine:1b", "", "", nil))
	sb := testBoard(t, results, testCorpus)
	if got := unrankedOf(t, sb, "old-engine:1b"); got != "engine before 0.5.0" {
		t.Fatalf("unranked %q", got)
	}
	if got := unrankedOf(t, sb, "unknown-engine:1b"); got != "no engine block" {
		t.Fatalf("a result without an engine block: %q", got)
	}
	md := sb.Markdown()
	if !strings.Contains(md, "- old-engine:1b on ollama 0.35.0: engine before 0.5.0, which named no origin\n") ||
		!strings.Contains(md, "- unknown-engine:1b on ollama 0.35.0: no engine block\n") {
		t.Fatalf("markdown:\n%s", md)
	}
	sb = testBoard(t, append(hardwareResults(), outsideResult("odd:1b", "elsewhere", "", nil)), testCorpus)
	if got := unrankedOf(t, sb, "odd:1b"); got != `origin "elsewhere"` {
		t.Fatalf("unranked %q", got)
	}
}

func TestLintFlagsAResultsFileNamingAMissingOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	results := t.TempDir()
	r := outsideResult("nemotron-3.5-bolt:74b", "registry", bolt, nil)
	if _, err := WriteResults(results, r); err != nil {
		t.Fatal(err)
	}
	problems := LintResults(results)
	if len(problems) != 1 || !strings.Contains(problems[0], "nemotron-3.5-bolt:74b") || !strings.Contains(problems[0], bolt) {
		t.Fatalf("problems %v", problems)
	}
	if err := os.MkdirAll(filepath.Dir(bolt), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bolt, []byte("models: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if problems := LintResults(results); len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	r.Override = "overrides/x.registry.yaml"
	r.Model = "other:1b"
	if _, err := WriteResults(results, r); err != nil {
		t.Fatal(err)
	}
	plain := r // a results file without an override has nothing to check
	plain.Override, plain.Model = "", "plain:1b"
	if _, err := WriteResults(results, plain); err != nil {
		t.Fatal(err)
	}
	if problems := LintResults(results); len(problems) != 1 || !strings.Contains(problems[0], "other:1b") {
		t.Fatalf("a path outside the published folder: %v", problems)
	}
	if problems := LintResults(filepath.Join(results, "none")); len(problems) != 1 {
		t.Fatalf("a results directory that cannot be read: %v", problems)
	}
}

func TestCheckOverrideTakesAPublishedFileOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, p := range []string{"../x.registry.yaml", "docs/evals/x.registry.yaml", "docs/evals/overrides/../x.registry.yaml",
		"docs/evals/overrides/x.yaml", "/docs/evals/overrides/x.registry.yaml", "docs/evals/overrides/.registry.yaml", ""} {
		if err := CheckOverride(p); err == nil {
			t.Fatalf("%q taken", p)
		}
	}
	if err := CheckOverride(bolt); err == nil || !strings.Contains(err.Error(), bolt) {
		t.Fatalf("a file that is not there: %v", err)
	}
	if err := os.MkdirAll(bolt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CheckOverride(bolt); err == nil {
		t.Fatal("a directory")
	}
	if err := os.Remove(bolt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bolt, []byte("models: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckOverride(bolt); err != nil {
		t.Fatal(err)
	}
}

func TestEngineSourceLabelNamesTheFile(t *testing.T) {
	for _, c := range []struct {
		source *EngineSource
		want   string
	}{
		{nil, ""},
		{&EngineSource{Repo: "unsloth/Qwen3-GGUF", File: "q4.gguf"}, "unsloth/Qwen3-GGUF/q4.gguf"},
		{&EngineSource{File: "q4.gguf"}, "q4.gguf"},
		{&EngineSource{Path: "local.gguf"}, "local.gguf"},
		{&EngineSource{SHA256: testSHA}, ""},
	} {
		if got := c.source.Label(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.source, got, c.want)
		}
	}
}

func TestAnOutsideRowIsNeverATierDefault(t *testing.T) {
	r := outsideResult("nemotron-3.5-bolt:74b", "registry", bolt, nil)
	r.Default = true // the override's entry marked itself the tier default
	sb := testBoard(t, append(hardwareResults(), r), testCorpus)
	for _, tier := range sb.Tiers {
		for _, row := range tier.Rows {
			if row.Model == "nemotron-3.5-bolt:74b" && row.Default {
				t.Fatalf("an outside row is the %s tier's default", tier.Tier)
			}
		}
	}
	if md := sb.Markdown(); strings.Contains(md, "nemotron-3.5-bolt:74b [1] (default)") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestAnImportWithAnOverrideLinksItsFile(t *testing.T) {
	src := &EngineSource{Repo: "unsloth/Qwen3-GGUF", File: "q4.gguf", SHA256: testSHA}
	sb := testBoard(t, append(hardwareResults(), outsideResult("qwen-import", "import", bolt, src)), testCorpus)
	md := sb.Markdown()
	if !strings.Contains(md, "[1] outside the catalog: unsloth/Qwen3-GGUF/q4.gguf, sha256 0123456789ab\n") || strings.Contains(md, bolt) {
		t.Fatalf("an import is named by its file, never by an override:\n%s", md)
	}
}

func TestTheUnrankedListIsNotARanking(t *testing.T) {
	onEngine := outsideResult("alpha:1b", "", "", nil) // the same model on a second server lists after its ollama row
	onEngine.Ollama, onEngine.Server = "", &ServerInfo{Name: "taraengine", Version: "0.4.1"}
	results := append(hardwareResults(), outsideResult("zeta:1b", "", "", nil), onEngine, outsideResult("alpha:1b", "", "", nil))
	md := testBoard(t, results, testCorpus).Markdown()
	i, i2, j, k := strings.Index(md, "- alpha:1b on ollama"), strings.Index(md, "- alpha:1b on taraengine"),
		strings.Index(md, "- unknown-engine:1b on"), strings.Index(md, "- zeta:1b on")
	if i < 0 || i2 < 0 || j < 0 || k < 0 || i > i2 || i2 > j || j > k {
		t.Fatalf("the list under the board is by name and server, not by score:\n%s", md)
	}
}

func TestAShortSHAInAResultsFileDoesNotStopTheReport(t *testing.T) {
	src := &EngineSource{File: "q4.gguf", SHA256: "abc"}
	md := testBoard(t, append(hardwareResults(), outsideResult("odd-import", "import", "", src)), testCorpus).Markdown()
	if !strings.Contains(md, "[1] outside the catalog: q4.gguf, sha256 abc\n") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestAnUnrankedResultWithoutAMachineIsListedAsSkipped(t *testing.T) {
	r := Results{Taracode: "3.3.3", Server: &ServerInfo{Name: "taraengine", Version: "0.4.1"}, Model: "gemma4:12b", Tier: "16",
		Think: "auto", Date: "2026-10-20", Runs: 1, Engine: &EngineInfo{GPUPercent: 100},
		Summary: Summary{PassRate: 0.9, MeanScore: 0.9, ByArea: map[string]AreaSummary{}}}
	sb := BuildScoreboard([]Results{r}, 33, "3.4.0")
	if len(sb.Tiers) != 0 || len(sb.Boards) != 0 || len(sb.Skipped) != 1 ||
		sb.Skipped[0] != "gemma4:12b on taraengine 0.4.1 2026-10-20: not ranked, engine before 0.5.0" {
		t.Fatalf("tiers %d boards %d skipped %q", len(sb.Tiers), len(sb.Boards), sb.Skipped)
	}
}

func TestPublishedResultsNameExistingOverrides(t *testing.T) {
	t.Chdir("../..") // the repository root, where the override paths resolve
	if problems := LintResults("docs/evals/results"); len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
}
