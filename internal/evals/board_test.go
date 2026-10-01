package evals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/models"
)

const testHardware = "NVIDIA RTX 5090 (32 GB)"

// testCorpus is the corpus size the board fixtures cover: hardwareResult gives every result two tasks.
const testCorpus = 2

func testEngine(size, vram int64, pct int) *EngineInfo {
	return &EngineInfo{SizeBytes: size, VRAMBytes: vram, GPUPercent: pct, ContextLength: 32768,
		Quantization: "Q4_K_M", ParameterSize: "29.9B", Digest: "4475827791a2"}
}

// onGPU is a small model entirely on the GPU, for tests that are not about memory.
func onGPU() *EngineInfo { return testEngine(1_000_000_000, 1_000_000_000, 100) }

func hardwareResult(model, tier string, pass, score, tps float64, engine *EngineInfo) Results {
	return Results{Taracode: "v3.2.0", Ollama: "0.35.0", Model: model, Tier: tier, Think: "auto", Date: "2026-10-01",
		Runs: 1, Hardware: testHardware, Engine: engine,
		Tasks:   []TaskResult{{ID: "a"}, {ID: "b"}},
		Summary: Summary{PassRate: pass, MeanScore: score, MeanWallMs: 5000, TokensPerS: tps, ByArea: map[string]AreaSummary{}}}
}

func hardwareResults() []Results {
	return []Results{
		hardwareResult("qwen3.8:27b", "32", 0.94, 0.96, 60, testEngine(22_100_000_000, 22_100_000_000, 100)),
		hardwareResult("glm-4.7-flash", "32", 0.94, 0.96, 140, testEngine(20_720_000_000, 20_720_000_000, 100)),
		hardwareResult("gemma4:12b", "16", 0.82, 0.88, 95, testEngine(9_300_000_000, 9_300_000_000, 100)),
		hardwareResult("laguna-xs-2.1", "other", 0.97, 0.95, 120, testEngine(20_600_000_000, 20_600_000_000, 100)),
		hardwareResult("huge:70b", "other", 0.99, 0.99, 8, testEngine(44_000_000_000, 33_000_000_000, 75)),
		hardwareResult("unknown-engine:1b", "other", 0.5, 0.5, 0, nil),
		{Taracode: "v3.1.1", Ollama: "0.34.2", Model: "qwen3.5:9b", Tier: "16", Think: "auto", Date: "2026-09-25", Runs: 1,
			Summary: Summary{PassRate: 0.85, MeanScore: 0.92, ByArea: map[string]AreaSummary{}}}, // no hardware label
	}
}

func boardModels(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Model)
	}
	return out
}

func testBoard(t *testing.T, results []Results, corpusTasks int) Scoreboard {
	t.Helper()
	reg, err := models.Load()
	if err != nil {
		t.Fatal(err)
	}
	return BuildScoreboard(results, reg, corpusTasks, "v3.2.0")
}

// glmRowWrong is TestBuildScoreboardRanksAHardwareBoard's check of one whole row, split out to keep
// the test under the gocyclo threshold.
func glmRowWrong(r Row) bool {
	return r.Tier != "32" || !r.Default || r.TokensPerS != 140 || r.VRAMGB != 20.7 || r.SizeGB != 20.7 ||
		r.GPUPercent == nil || *r.GPUPercent != 100 || r.ContextLength != 32768 || r.Quantization != "Q4_K_M" ||
		r.ParameterSize != "29.9B" || r.Digest != "4475827791a2" || r.Runs != 1 || r.SuiteWallS != 10 ||
		r.Hardware != testHardware || r.Tasks != 2
}

func TestBuildScoreboardRanksAHardwareBoard(t *testing.T) {
	sb := testBoard(t, hardwareResults(), testCorpus)
	if len(sb.Boards) != 1 || sb.Boards[0].Hardware != testHardware {
		t.Fatalf("boards %+v", sb.Boards)
	}
	board := sb.Boards[0]
	// Pass rate first; glm-4.7-flash and qwen3.8:27b tie on pass rate and score, the faster one leads;
	// a row with no engine block stays ranked; the row that is 75% on the GPU is not ranked.
	want := []string{"laguna-xs-2.1", "glm-4.7-flash", "qwen3.8:27b", "gemma4:12b", "unknown-engine:1b"}
	if got := boardModels(board.Rows); !slices.Equal(got, want) {
		t.Fatalf("ranking %v, want %v", got, want)
	}
	if dnf := board.DidNotFit; len(dnf) != 1 || dnf[0].Model != "huge:70b" || *dnf[0].GPUPercent != 75 || dnf[0].SizeGB != 44 {
		t.Fatalf("did not fit %+v", dnf)
	}
	if glm := board.Rows[1]; glmRowWrong(glm) {
		t.Fatalf("row %+v", glm)
	}
	if e := board.Rows[4]; e.GPUPercent != nil || e.VRAMGB != 0 || e.SizeGB != 0 {
		t.Fatalf("a row without an engine block has memory numbers: %+v", e)
	}
	// The tier tables still hold every model, labelled or not.
	if len(sb.Tiers) != 3 || len(sb.Tiers[0].Rows) != 2 || sb.Tiers[0].Rows[0].Model != "qwen3.5:9b" {
		t.Fatalf("tiers %+v", sb.Tiers)
	}
}

// TestBoardRanksByScoreThenSpeedThenName pins the second, third and last key of the ordering: with
// the pass rate level, the mean score decides before the speed, and the name settles a full tie.
func TestBoardRanksByScoreThenSpeedThenName(t *testing.T) {
	sb := testBoard(t, []Results{
		hardwareResult("fast-low-score:1b", "other", 0.9, 0.80, 500, onGPU()),
		hardwareResult("zeta:1b", "other", 0.9, 0.90, 99, onGPU()),
		hardwareResult("slow-high-score:1b", "other", 0.9, 0.95, 10, onGPU()),
		hardwareResult("alpha:1b", "other", 0.9, 0.90, 99, onGPU()),
		hardwareResult("quick:1b", "other", 0.9, 0.90, 120, onGPU()),
	}, testCorpus)
	want := []string{"slow-high-score:1b", "quick:1b", "alpha:1b", "zeta:1b", "fast-low-score:1b"}
	if got := boardModels(sb.Boards[0].Rows); !slices.Equal(got, want) {
		t.Fatalf("ranking %v, want %v", got, want)
	}
}

func TestBoardsSortByLabelAndDidNotFitRowsByRank(t *testing.T) {
	mac := hardwareResult("m4:1b", "other", 0.5, 0.5, 10, onGPU())
	mac.Hardware = "Apple M4 Max (64 GB)"
	low := hardwareResult("low:70b", "other", 0.3, 0.3, 5, testEngine(44_000_000_000, 33_000_000_000, 75))
	high := hardwareResult("high:70b", "other", 0.8, 0.8, 5, testEngine(44_000_000_000, 33_000_000_000, 75))
	sb := testBoard(t, append(hardwareResults(), low, mac, high), testCorpus)
	if len(sb.Boards) != 2 || sb.Boards[0].Hardware != "Apple M4 Max (64 GB)" || sb.Boards[1].Hardware != testHardware {
		t.Fatalf("boards %+v", sb.Boards)
	}
	want := []string{"huge:70b", "high:70b", "low:70b"}
	if got := boardModels(sb.Boards[1].DidNotFit); !slices.Equal(got, want) {
		t.Fatalf("did not fit %v, want %v", got, want)
	}
}

// TestPartialCorpusResultsAreNotRanked: a results file from a subset run (--tasks) must never take a
// place in the ranking next to runs of the whole corpus. It is listed apart, with its task count.
func TestPartialCorpusResultsAreNotRanked(t *testing.T) {
	sb := testBoard(t, hardwareResults(), 33) // every fixture ran 2 tasks, the corpus has 33
	board := sb.Boards[0]
	if len(board.Rows) != 0 || len(board.DidNotFit) != 0 || len(board.Partial) != 6 {
		t.Fatalf("ranked %d, did not fit %d, partial %d", len(board.Rows), len(board.DidNotFit), len(board.Partial))
	}
	md := sb.Markdown()
	for _, want := range []string{
		"Not ranked, ran only part of the corpus:",
		"- laguna-xs-2.1: 2 of 33 tasks",
		"- huge:70b: 2 of 33 tasks",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "| # | Model |") || strings.Contains(md, "| 1 | ") {
		t.Errorf("a partial run was put in a ranking table:\n%s", md)
	}
}

// TestEmptyRankingPrintsNoTable: a machine on which no model sat entirely on the GPU gets no empty
// table and no "0 models" line.
func TestEmptyRankingPrintsNoTable(t *testing.T) {
	cpuOnly := hardwareResult("cpu-only:1b", "other", 0.9, 0.9, 3, testEngine(5_000_000_000, 0, 0))
	md := testBoard(t, []Results{cpuOnly}, testCorpus).Markdown()
	for _, want := range []string{
		"No model ran entirely on this GPU.",
		"- cpu-only:1b: the engine reports 5.0 GB loaded, 0% on the GPU, pass rate 90%",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "| # | Model |") || strings.Contains(md, "0 models") {
		t.Errorf("an empty ranking still prints its table:\n%s", md)
	}
}

// TestTheNewestResultDecidesBoardMembership: a model's newest result is its only row, so a newer
// result without a hardware label takes the model off the board instead of leaving a stale row.
func TestTheNewestResultDecidesBoardMembership(t *testing.T) {
	old := hardwareResult("gemma4:12b", "16", 0.82, 0.88, 95, testEngine(9_300_000_000, 9_300_000_000, 100))
	newer := Results{Taracode: "v3.2.0", Ollama: "0.35.0", Model: "gemma4:12b", Tier: "16", Think: "auto", Date: "2026-10-02",
		Runs: 1, Summary: Summary{PassRate: 0.9, MeanScore: 0.9, ByArea: map[string]AreaSummary{}}}
	sb := testBoard(t, []Results{old, newer}, testCorpus)
	if len(sb.Boards) != 0 {
		t.Fatalf("a stale row stayed on the board: %+v", sb.Boards)
	}
	if rows := sb.Tiers[0].Rows; len(rows) != 1 || rows[0].PassRate != 0.9 {
		t.Fatalf("tier rows %+v", rows)
	}
}

func TestScoreboardWithoutHardwareLabelsHasNoBoards(t *testing.T) {
	sb := testBoard(t, sampleResults(), 33)
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(sb.Boards) != 0 || strings.Contains(string(data), `"boards"`) {
		t.Fatalf("boards in a scoreboard built from unlabelled results: %s", data)
	}
}

// TestScoreboardJSONCarriesTheKeysTheSiteReads pins the JSON names code.tara.vision reads: the tests
// above check struct fields, which a renamed tag would pass.
func TestScoreboardJSONCarriesTheKeysTheSiteReads(t *testing.T) {
	data, err := testBoard(t, hardwareResults(), testCorpus).JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"generated_at", "taracode", "corpus_tasks", "boards", "tiers", "hardware", "rows",
		"did_not_fit", "model", "default", "tier", "pass_rate", "mean_score", "mean_wall_s", "tokens_per_s", "vram_gb",
		"size_gb", "gpu_percent", "quantization", "runs", "tasks", "ollama", "date", "by_area", "fixture_miss_rate",
		"mean_iterations"} {
		if !strings.Contains(string(data), `"`+key+`":`) {
			t.Errorf("scoreboard.json lacks the key %q", key)
		}
	}
}

// TestPublishedResultsRoundTripUnchanged: every results file in the repository reads into Results and
// writes back byte for byte, so nothing this version adds changes a file an older version wrote.
func TestPublishedResultsRoundTripUnchanged(t *testing.T) {
	files, err := filepath.Glob("../../docs/evals/results/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no published results to check: %v", err)
	}
	for _, f := range files {
		want, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r Results
		if err := json.Unmarshal(want, &r); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		got, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if string(append(got, '\n')) != string(want) {
			t.Errorf("%s does not round trip byte for byte", f)
		}
	}
}

func TestScoreboardMarkdownPutsTheHardwareBoardFirst(t *testing.T) {
	md := testBoard(t, hardwareResults(), testCorpus).Markdown()
	for _, want := range []string{
		"\n## NVIDIA RTX 5090 (32 GB)\n",
		"5 models, Ollama 0.35.0, context window 32768, one run each.",
		"| # | Model | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |",
		"| 1 | laguna-xs-2.1 | - | 97% | 0.95 | 120 | 5 s | ~19.2 GiB | Q4_K_M | 1 |",
		"| 2 | glm-4.7-flash | 32 GB | 94% | 0.96 | 140 | 5 s | ~19.3 GiB | Q4_K_M | 1 |",
		"| 5 | unknown-engine:1b | - | 50% | 0.50 | - | 5 s | - | - | 1 |",
		"- huge:70b: the engine reports 44.0 GB loaded, 75% on the GPU, pass rate 99%",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Index(md, "## NVIDIA RTX 5090") > strings.Index(md, "## 16 GB tier") {
		t.Error("the hardware board comes after the tier tables")
	}
}

// TestBoardFactsNameMixedRuns: when a finalist was re-run, the board says where the run counts are.
func TestBoardFactsNameMixedRuns(t *testing.T) {
	results := hardwareResults()
	results[1].Runs = 3
	md := testBoard(t, results, testCorpus).Markdown()
	if !strings.Contains(md, "runs per model in the last column.") || strings.Contains(md, "one run each") {
		t.Fatalf("facts line does not name the mixed runs:\n%s", md)
	}
	if !strings.Contains(md, "| 2 | glm-4.7-flash | 32 GB | 94% | 0.96 | 140 | 5 s | ~19.3 GiB | Q4_K_M | 3 |") {
		t.Fatalf("the re-run row does not show its run count:\n%s", md)
	}
}

func TestBoardFactsSortContextWindowsByNumber(t *testing.T) {
	big := hardwareResult("big-window:1b", "other", 0.9, 0.9, 10, onGPU())
	big.Engine.ContextLength = 131072
	small := hardwareResult("small-window:1b", "other", 0.8, 0.8, 10, onGPU())
	small.Engine.ContextLength = 8192
	md := testBoard(t, []Results{big, small, hardwareResult("usual:1b", "other", 0.7, 0.7, 10, onGPU())}, testCorpus).Markdown()
	if !strings.Contains(md, "context window 8192, 32768 and 131072,") {
		t.Fatalf("the context windows are not in numeric order:\n%s", md)
	}
}

// TestBoardMarkdownEscapesWhatAResultsFileCarries: the report trusts no string a results file holds,
// since a file can be edited or come from someone else. A pipe must not split a row and a newline
// must not start a heading.
func TestBoardMarkdownEscapesWhatAResultsFileCarries(t *testing.T) {
	evil := hardwareResult("evil|model\n## injected", "other", 0.9, 0.9, 10, onGPU())
	evil.Hardware = "GPU\n## also injected"
	evil.Engine.Quantization = "Q4|x"
	md := testBoard(t, []Results{evil}, testCorpus).Markdown()
	if strings.Contains(md, "\n## injected") || strings.Contains(md, "\n## also injected") {
		t.Fatalf("a results file injected a heading:\n%s", md)
	}
	for _, want := range []string{"\n## GPU ## also injected\n", `evil\|model ## injected`, `Q4\|x`} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

// TestUncheckedCountsRankedRowsWithoutMemoryData: a ranked row the engine described nothing for was
// never checked for fit, and the report says how many there are.
func TestUncheckedCountsRankedRowsWithoutMemoryData(t *testing.T) {
	board := testBoard(t, hardwareResults(), testCorpus).Boards[0]
	if got := board.Unchecked(); got != 1 { // unknown-engine:1b
		t.Fatalf("unchecked %d, want 1", got)
	}
	if got := (HardwareBoard{}).Unchecked(); got != 0 {
		t.Fatalf("an empty board counts %d unchecked rows", got)
	}
}

// TestBoardNamesTheSmallTier: the registry's small tier has no size, so the Tier column says "small".
func TestBoardNamesTheSmallTier(t *testing.T) {
	md := testBoard(t, []Results{hardwareResult("gemma4:e4b", "small", 0.7, 0.8, 150, onGPU())}, testCorpus).Markdown()
	if !strings.Contains(md, "| 1 | gemma4:e4b | small | 70% | 0.80 | 150 | 5 s | ~0.9 GiB | Q4_K_M | 1 |") {
		t.Fatalf("the small tier is not named:\n%s", md)
	}
}
