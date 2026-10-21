package evals

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// catalogEngine is the engine block of a catalog model, which every ranked row needs (spec 1h).
func catalogEngine() *EngineInfo { return &EngineInfo{Origin: "catalog", GPUPercent: 100} }

func sampleResults() []Results {
	area := func(tasks, passed int, score float64) AreaSummary {
		return AreaSummary{Tasks: tasks, Passed: passed, MeanScore: score}
	}
	return []Results{
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "gemma4:12b", Tier: "16", Default: true, Think: "auto", Date: "2026-09-26", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.7, MeanScore: 0.74, MeanIterations: 5.2, MeanWallMs: 21000, FixtureMissRate: 0.05,
				ByArea: map[string]AreaSummary{"kubernetes": area(9, 7, 0.8), "refusal": area(6, 5, 0.85)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "gemma4:12b", Tier: "16", Think: "auto", Date: "2026-09-25", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.5, MeanScore: 0.6, ByArea: map[string]AreaSummary{}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "qwen3.5:9b", Tier: "16", Think: "auto", Date: "2026-09-26", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.8, MeanScore: 0.82, MeanIterations: 4.1, MeanWallMs: 15000, ByArea: map[string]AreaSummary{"kubernetes": area(9, 8, 0.9)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "qwen3.8:27b", Tier: "32", Think: "auto", Date: "2026-09-26", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.9, MeanScore: 0.91, MeanIterations: 3.8, MeanWallMs: 30000, ByArea: map[string]AreaSummary{"kubernetes": area(9, 9, 0.95)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "glm-4.7-flash", Tier: "32", Default: true, Think: "auto", Date: "2026-09-26", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.97, MeanScore: 0.96, MeanIterations: 5.4, MeanWallMs: 5000, FixtureMissRate: 0.25,
				ByArea: map[string]AreaSummary{"kubernetes": area(9, 9, 0.99), "refusal": area(6, 5, 0.9)}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "mystery:1b", Tier: "other", Think: "auto", Date: "2026-09-26", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.1, MeanScore: 0.2, ByArea: map[string]AreaSummary{}}},
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "qwen3.6:35b", Tier: "48", Think: "auto", Date: "2026-09-26", Runs: 1, Engine: catalogEngine(),
			Summary: Summary{PassRate: 0.9, MeanScore: 0.9, SafetyFailures: 1, ByArea: map[string]AreaSummary{}}},
	}
}

func TestBuildScoreboardKeepsTheNewestPerModelAndSkipsSafetyFailures(t *testing.T) {
	sb := BuildScoreboard(sampleResults(), 33, "3.0.0-beta.1")
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
	checks := sb.CheckDefaults() // the other tier is not a sized tier and is skipped
	if len(checks) != 2 || !strings.Contains(checks[0], "16: default gemma4:12b, top scorer qwen3.5:9b (differs)") ||
		!strings.Contains(checks[1], "32: default glm-4.7-flash, top scorer glm-4.7-flash") {
		t.Fatalf("checks %q", checks)
	}
}

// TestScoreboardJSONRoundTripKeepsTierOrderAndDefaults pins the JSON the site reads: rows stay sorted by
// mean score inside a tier and exactly the model the results mark as the default carries the marker.
func TestScoreboardJSONRoundTripKeepsTierOrderAndDefaults(t *testing.T) {
	sb := BuildScoreboard(sampleResults(), 33, "3.0.0-beta.1")
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back Scoreboard
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	rows := back.Tiers[1].Rows
	if len(rows) != 2 || rows[0].Model != "glm-4.7-flash" || !rows[0].Default || rows[1].Model != "qwen3.8:27b" || rows[1].Default {
		t.Fatalf("32 GB rows after the round trip: %+v", rows)
	}
}

func TestScoreboardMarkdownMatchesTheGolden(t *testing.T) {
	sb := BuildScoreboard(sampleResults(), 33, "3.0.0-beta.1")
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

func TestEmptyScoreboardSerializesEmptyTiers(t *testing.T) {
	sb := BuildScoreboard(nil, 0, "3.0.0-beta.1")
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tiers": []`) {
		t.Fatalf("expected empty tiers array, got: %s", string(data))
	}
}

func TestRowWithNilByAreaSerializesAsEmptyMap(t *testing.T) {
	results := []Results{
		{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "test:1b", Tier: "16", Think: "auto", Date: "2026-09-26", Runs: 1,
			Engine: catalogEngine(), Summary: Summary{PassRate: 0.5, MeanScore: 0.5, ByArea: nil}},
	}
	sb := BuildScoreboard(results, 1, "3.0.0-beta.1")
	data, err := sb.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"by_area": {}`) {
		t.Fatalf("expected empty by_area map, got: %s", string(data))
	}
}

func TestTierOrderIsDeterministic(t *testing.T) {
	for i := 0; i < 50; i++ {
		results := []Results{
			{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "zzz:1b", Tier: "zzz-tier", Think: "auto", Date: "2026-09-26", Runs: 1,
				Engine: catalogEngine(), Summary: Summary{PassRate: 0.5, MeanScore: 0.5, ByArea: map[string]AreaSummary{}}},
			{Taracode: "3.0.0-beta.1", Ollama: "0.34.2", Model: "aaa:1b", Tier: "aaa-tier", Think: "auto", Date: "2026-09-26", Runs: 1,
				Engine: catalogEngine(), Summary: Summary{PassRate: 0.5, MeanScore: 0.5, ByArea: map[string]AreaSummary{}}},
		}
		sb := BuildScoreboard(results, 2, "3.0.0-beta.1")
		if len(sb.Tiers) != 2 || sb.Tiers[0].Tier != "aaa-tier" || sb.Tiers[1].Tier != "zzz-tier" {
			t.Fatalf("iteration %d: expected aaa-tier before zzz-tier, got %+v", i, sb.Tiers)
		}
	}
}

func TestTierTablesRankByPassRateThenScoreThenSpeed(t *testing.T) {
	row := func(model, hardware string, pass, score, tps float64) Results {
		return Results{Taracode: "3.4.0", Server: &ServerInfo{Name: "taraengine", Version: "0.5.0"}, Model: model,
			Tier: "32", Think: "auto", Date: "2026-11-01", Runs: 1, Hardware: hardware, Engine: catalogEngine(),
			Summary: Summary{PassRate: pass, MeanScore: score, TokensPerS: tps, ByArea: map[string]AreaSummary{}}}
	}
	all := []Results{
		row("high-score", "", 0.85, 0.95, 100),
		row("high-pass", "", 0.91, 0.90, 100),
		row("tie-slow", "", 0.91, 0.90, 50),
		row("tie-fast", "", 0.91, 0.90, 200),
		row("same", "Apple M4 Max (48 GB)", 0.80, 0.80, 10),
		row("same", "NVIDIA RTX 5090 (32 GB)", 0.80, 0.80, 10),
	}
	sb := BuildScoreboard(all, 33, "3.4.0")
	var got []string
	for _, r := range sb.Tiers[0].Rows {
		got = append(got, r.Model+"@"+r.Hardware)
	}
	want := []string{"tie-fast@", "high-pass@", "tie-slow@", "high-score@", "same@Apple M4 Max (48 GB)", "same@NVIDIA RTX 5090 (32 GB)"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("tier order:\n got %v\nwant %v", got, want)
	}
}
