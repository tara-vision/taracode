package evals

import (
	"slices"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/models"
)

const testHardware = "NVIDIA RTX 5090 (32 GB)"

func testEngine(size, vram int64, pct int) *EngineInfo {
	return &EngineInfo{SizeBytes: size, VRAMBytes: vram, GPUPercent: pct, ContextLength: 32768,
		Quantization: "Q4_K_M", ParameterSize: "29.9B", Digest: "4475827791a2"}
}

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

func TestBuildScoreboardRanksAHardwareBoard(t *testing.T) {
	reg, err := models.Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := BuildScoreboard(hardwareResults(), reg, 33, "v3.2.0")
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
	glm := board.Rows[1]
	if glm.Tier != "32" || !glm.Default || glm.TokensPerS != 140 || glm.VRAMGB != 20.7 || glm.SizeGB != 20.7 ||
		*glm.GPUPercent != 100 || glm.ContextLength != 32768 || glm.Quantization != "Q4_K_M" ||
		glm.ParameterSize != "29.9B" || glm.Digest != "4475827791a2" || glm.Runs != 1 || glm.SuiteWallS != 10 ||
		glm.Hardware != testHardware {
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

// TestTheNewestResultDecidesBoardMembership: a model's newest result is its only row, so a newer
// result without a hardware label takes the model off the board instead of leaving a stale row.
func TestTheNewestResultDecidesBoardMembership(t *testing.T) {
	reg, _ := models.Load()
	old := hardwareResult("gemma4:12b", "16", 0.82, 0.88, 95, testEngine(9_300_000_000, 9_300_000_000, 100))
	newer := Results{Taracode: "v3.2.0", Ollama: "0.35.0", Model: "gemma4:12b", Tier: "16", Think: "auto", Date: "2026-10-02",
		Runs: 1, Summary: Summary{PassRate: 0.9, MeanScore: 0.9, ByArea: map[string]AreaSummary{}}}
	sb := BuildScoreboard([]Results{old, newer}, reg, 33, "v3.2.0")
	if len(sb.Boards) != 0 {
		t.Fatalf("a stale row stayed on the board: %+v", sb.Boards)
	}
	if rows := sb.Tiers[0].Rows; len(rows) != 1 || rows[0].PassRate != 0.9 {
		t.Fatalf("tier rows %+v", rows)
	}
}

func TestScoreboardWithoutHardwareLabelsHasNoBoards(t *testing.T) {
	reg, _ := models.Load()
	sb := BuildScoreboard(sampleResults(), reg, 33, "3.0.0-beta.1")
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(sb.Boards) != 0 || strings.Contains(string(data), `"boards"`) {
		t.Fatalf("boards in a scoreboard built from unlabelled results: %s", data)
	}
}

func TestScoreboardMarkdownPutsTheHardwareBoardFirst(t *testing.T) {
	reg, _ := models.Load()
	md := BuildScoreboard(hardwareResults(), reg, 33, "v3.2.0").Markdown()
	for _, want := range []string{
		"\n## NVIDIA RTX 5090 (32 GB)\n",
		"5 models, Ollama 0.35.0, context window 32768, one run each.",
		"| # | Model | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |",
		"| 1 | laguna-xs-2.1 | - | 97% | 0.95 | 120 | 5 s | 20.6 GB | Q4_K_M | 1 |",
		"| 2 | glm-4.7-flash | 32 GB | 94% | 0.96 | 140 | 5 s | 20.7 GB | Q4_K_M | 1 |",
		"| 5 | unknown-engine:1b | - | 50% | 0.50 | - | 5 s | - | - | 1 |",
		"- huge:70b: 44.0 GB loaded, 75% on the GPU, pass rate 99%",
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
	reg, _ := models.Load()
	results := hardwareResults()
	results[1].Runs = 3
	md := BuildScoreboard(results, reg, 33, "v3.2.0").Markdown()
	if !strings.Contains(md, "runs per model in the last column.") || strings.Contains(md, "one run each") {
		t.Fatalf("facts line does not name the mixed runs:\n%s", md)
	}
	if !strings.Contains(md, "| 2 | glm-4.7-flash | 32 GB | 94% | 0.96 | 140 | 5 s | 20.7 GB | Q4_K_M | 3 |") {
		t.Fatalf("the re-run row does not show its run count:\n%s", md)
	}
}
