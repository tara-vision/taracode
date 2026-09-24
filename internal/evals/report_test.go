package evals

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/models"
)

func sampleResults() []Results {
	area := func(tasks, passed int, score float64) AreaSummary {
		return AreaSummary{Tasks: tasks, Passed: passed, MeanScore: score}
	}
	return []Results{
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "gemma4:12b", Tier: "16", Think: "auto", Date: "2026-09-26", Runs: 1,
			Summary: Summary{PassRate: 0.7, MeanScore: 0.74, MeanIterations: 5.2, MeanWallMs: 21000, FixtureMissRate: 0.05,
				ByArea: map[string]AreaSummary{"kubernetes": area(9, 7, 0.8), "refusal": area(6, 5, 0.85)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "gemma4:12b", Tier: "16", Think: "auto", Date: "2026-09-25", Runs: 1,
			Summary: Summary{PassRate: 0.5, MeanScore: 0.6, ByArea: map[string]AreaSummary{}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "qwen3.5:9b", Tier: "16", Think: "auto", Date: "2026-09-26", Runs: 1,
			Summary: Summary{PassRate: 0.8, MeanScore: 0.82, MeanIterations: 4.1, MeanWallMs: 15000, ByArea: map[string]AreaSummary{"kubernetes": area(9, 8, 0.9)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "qwen3.8:27b", Tier: "32", Think: "auto", Date: "2026-09-26", Runs: 1,
			Summary: Summary{PassRate: 0.9, MeanScore: 0.91, MeanIterations: 3.8, MeanWallMs: 30000, ByArea: map[string]AreaSummary{"kubernetes": area(9, 9, 0.95)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "mystery:1b", Tier: "other", Think: "auto", Date: "2026-09-26", Runs: 1,
			Summary: Summary{PassRate: 0.1, MeanScore: 0.2, ByArea: map[string]AreaSummary{}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "qwen3.6:35b", Tier: "48", Think: "auto", Date: "2026-09-26", Runs: 1,
			Summary: Summary{PassRate: 0.9, MeanScore: 0.9, SafetyFailures: 1, ByArea: map[string]AreaSummary{}}},
	}
}

func TestBuildScoreboardKeepsTheNewestPerModelAndSkipsSafetyFailures(t *testing.T) {
	reg, err := models.Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := BuildScoreboard(sampleResults(), reg, 33, "3.0.0-beta.1")
	if sb.CorpusTasks != 33 || len(sb.Skipped) != 1 || !strings.Contains(sb.Skipped[0], "qwen3.6:35b") {
		t.Fatalf("%+v", sb)
	}
	if len(sb.Tiers) != 3 || sb.Tiers[0].Tier != "16" || sb.Tiers[1].Tier != "32" || sb.Tiers[2].Tier != "other" {
		t.Fatalf("tiers %+v", sb.Tiers)
	}
	rows := sb.Tiers[0].Rows
	if len(rows) != 2 || rows[0].Model != "qwen3.5:9b" || rows[1].Model != "gemma4:12b" || !rows[1].Default || rows[1].PassRate != 0.7 {
		t.Fatalf("16 GB rows %+v", rows)
	}
	checks := sb.CheckDefaults(reg) // the other tier has no registry default and is skipped
	if len(checks) != 2 || !strings.Contains(checks[0], "16: default gemma4:12b, top scorer qwen3.5:9b (differs)") ||
		!strings.Contains(checks[1], "32: default qwen3.8:27b, top scorer qwen3.8:27b") {
		t.Fatalf("checks %q", checks)
	}
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back Scoreboard
	if err := json.Unmarshal(data, &back); err != nil || back.Tiers[1].Rows[0].Model != "qwen3.8:27b" {
		t.Fatalf("json round trip: %v", err)
	}
}

func TestScoreboardMarkdownMatchesTheGolden(t *testing.T) {
	reg, _ := models.Load()
	sb := BuildScoreboard(sampleResults(), reg, 33, "3.0.0-beta.1")
	sb.GeneratedAt = "2026-09-26"
	got := sb.Markdown()
	want, err := os.ReadFile("testdata/scoreboard.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("markdown differs:\n%s", got)
	}
}
