package evals

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func serverResult(model, tier, server, version string, isDefault bool, score float64) Results {
	r := Results{Taracode: "3.3.0", Model: model, Tier: tier, Default: isDefault, Think: "auto", Date: "2026-10-25",
		Runs: 1, Summary: Summary{PassRate: score, MeanScore: score, ByArea: map[string]AreaSummary{}}}
	if server == "ollama" {
		r.Taracode, r.Ollama = "3.2.4", version
	} else {
		r.Server = &ServerInfo{Name: server, Version: version, Runtime: "b11514"}
	}
	return r
}

func TestBuildScoreboardKeepsOneRowPerModelAndServer(t *testing.T) {
	sb := BuildScoreboard([]Results{
		serverResult("gemma4:12b", "16", "taraengine", "0.2.0", true, 0.9),
		serverResult("gemma4:12b", "16", "ollama", "0.35.0", false, 0.8),
		serverResult("qwen3.6:27b", "16", "taraengine", "0.2.0", false, 0.95),
	}, 2, "3.3.0")
	rows := sb.Tiers[0].Rows
	if len(rows) != 3 {
		t.Fatalf("rows %+v", rows)
	}
	var engineRow, ollamaRow Row
	for _, r := range rows {
		switch r.Server.Name {
		case "taraengine":
			if r.Model == "gemma4:12b" {
				engineRow = r
			}
		case "ollama":
			ollamaRow = r
		}
	}
	if !engineRow.Default || !ollamaRow.Default || ollamaRow.Ollama != "0.35.0" || engineRow.Ollama != "" {
		t.Fatalf("the default marks both rows of the model: %+v %+v", engineRow, ollamaRow)
	}
	md := sb.Markdown()
	for _, want := range []string{"| Engine | Date |", "| taraengine 0.2.0 |", "| ollama 0.35.0 |",
		"--host <engine url>"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	data, _ := json.Marshal(engineRow)
	if !strings.Contains(string(data), `"server":{"name":"taraengine","version":"0.2.0","runtime":"b11514"}`) ||
		strings.Contains(string(data), `"ollama"`) {
		t.Fatalf("an engine row: %s", data)
	}
}

func TestCheckDefaultsReadsTheBoard(t *testing.T) {
	sb := BuildScoreboard([]Results{
		serverResult("gemma4:12b", "16", "taraengine", "0.2.0", true, 0.8),
		serverResult("qwen3.6:27b", "16", "taraengine", "0.2.0", false, 0.9),
		serverResult("glm-4.7-flash", "32", "taraengine", "0.2.0", false, 0.9),
	}, 2, "3.3.0")
	checks := sb.CheckDefaults()
	if len(checks) != 2 || checks[0] != "16: default gemma4:12b, top scorer qwen3.6:27b (differs)" ||
		checks[1] != "32: default none, top scorer glm-4.7-flash (differs)" {
		t.Fatalf("checks %q", checks)
	}
}

// onServer is the same run written by another server: the 3.2.x Ollama field gives way to the server block.
func onServer(r Results, name, version string) Results {
	r.Ollama = ""
	r.Server = &ServerInfo{Name: name, Version: version, Runtime: "b11514"}
	return r
}

func serverNames(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Server.Name)
	}
	return out
}

// TestOneModelOnTwoServersTiesOnTheServerName: the same scores on two servers make two rows, ordered by the
// server's name in the tier table and on the hardware board, whichever order the results arrive in
// (ReadResults leaves same-day results of one model in no defined order).
func TestOneModelOnTwoServersTiesOnTheServerName(t *testing.T) {
	ollama := hardwareResult("gemma4:12b", "16", 0.9, 0.9, 100, onGPU())
	engine := onServer(ollama, "taraengine", "0.2.0")
	want := []string{"ollama", "taraengine"}
	for _, in := range [][]Results{{ollama, engine}, {engine, ollama}} {
		sb := testBoard(t, in, testCorpus)
		if got := serverNames(sb.Tiers[0].Rows); !slices.Equal(got, want) {
			t.Errorf("tier rows by server %v, want %v", got, want)
		}
		if got := serverNames(sb.Boards[0].Rows); !slices.Equal(got, want) {
			t.Errorf("board rows by server %v, want %v", got, want)
		}
	}
}

// TestEachServerKeepsItsNewestResult: a newer result on one server never displaces another server's row, and
// the default mark is read from the newest results only.
func TestEachServerKeepsItsNewestResult(t *testing.T) {
	older := serverResult("gemma4:12b", "16", "taraengine", "0.2.0", true, 0.5)
	older.Date = "2026-10-24"
	newer := serverResult("gemma4:12b", "16", "taraengine", "0.2.1", false, 0.9)
	ollama := serverResult("gemma4:12b", "16", "ollama", "0.35.0", false, 0.8)
	ollama.Date = "2026-10-20"
	sb := BuildScoreboard([]Results{newer, ollama, older}, 2, "3.3.0")
	rows := sb.Tiers[0].Rows
	if len(rows) != 2 || rows[0].Server.String() != "taraengine 0.2.1" || rows[0].Date != "2026-10-25" ||
		rows[1].Server.String() != "ollama 0.35.0" || rows[1].Date != "2026-10-20" {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Default || rows[1].Default {
		t.Fatalf("the older result's default mark reached a row: %+v", rows)
	}
	if got := sb.CheckDefaults(); len(got) != 1 || got[0] != "16: default none, top scorer gemma4:12b (differs)" {
		t.Fatalf("checks %q", got)
	}
}

// TestBoardCountsModelsNotRows: a model that ran on two servers is two rows and one model, and the lines under the
// ranking name each row's server, so two rows of one model read apart; a row without a server is named alone.
func TestBoardCountsModelsNotRows(t *testing.T) {
	ollama := hardwareResult("a:1b", "other", 0.9, 0.9, 10, onGPU())
	engine := onServer(hardwareResult("a:1b", "other", 0.8, 0.8, 10, onGPU()), "taraengine", "0.3.0")
	bigOllama := hardwareResult("big:70b", "other", 0.99, 0.99, 8, testEngine(44_000_000_000, 33_000_000_000, 75))
	bigEngine := onServer(hardwareResult("big:70b", "other", 0.98, 0.98, 8, testEngine(44_000_000_000, 33_000_000_000, 75)),
		"taraengine", "0.3.0")
	other := onServer(hardwareResult("d:1b", "other", 0.6, 0.6, 10, onGPU()), "taraengine", "0.3.0")
	unnamed := hardwareResult("c:1b", "other", 0.7, 0.7, 10, testEngine(5_000_000_000, 0, 0))
	unnamed.Ollama = ""
	md := testBoard(t, []Results{ollama, engine, other, bigOllama, bigEngine, unnamed}, testCorpus).Markdown()
	for _, want := range []string{ // three ranked rows of two models
		"2 models, ollama 0.35.0 and taraengine 0.3.0, context window 32768, one run each.",
		"- big:70b on ollama 0.35.0: the engine reports 44.0 GB loaded, 75% on the GPU, pass rate 99%",
		"- big:70b on taraengine 0.3.0: the engine reports 44.0 GB loaded, 75% on the GPU, pass rate 98%",
		"- c:1b: the engine reports 5.0 GB loaded, 0% on the GPU, pass rate 70%",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "1 model,") || strings.Contains(md, "3 models,") {
		t.Errorf("the count is of rows, not models:\n%s", md)
	}
}

// TestBoardFactsNameEveryServer: the facts line lists each server once, name and version, and a result that
// names no server adds nothing to it and has an empty Engine cell.
func TestBoardFactsNameEveryServer(t *testing.T) {
	ollama := hardwareResult("a:1b", "other", 0.9, 0.9, 10, onGPU())
	engine := onServer(hardwareResult("b:1b", "other", 0.8, 0.8, 10, onGPU()), "taraengine", "0.2.0")
	unnamed := hardwareResult("c:1b", "other", 0.7, 0.7, 10, onGPU())
	unnamed.Ollama = ""
	md := testBoard(t, []Results{ollama, engine, unnamed}, testCorpus).Markdown()
	for _, want := range []string{
		"3 models, ollama 0.35.0 and taraengine 0.2.0, context window 32768, one run each.",
		"| 1 | a:1b | ollama 0.35.0 | - | 90% |",
		"| 2 | b:1b | taraengine 0.2.0 | - | 80% |",
		"| 3 | c:1b |  | - | 70% |",
		"| 5 s | 0% | taraengine 0.2.0 | 2026-10-01 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// TestMarkdownEscapesTheServerOfAResultsFile: the server a results file names goes through mdText wherever
// the report prints it: the board's facts line, its ranking and the tier table.
func TestMarkdownEscapesTheServerOfAResultsFile(t *testing.T) {
	evil := onServer(hardwareResult("m:1b", "other", 0.9, 0.9, 10, onGPU()), "evil|engine\n## injected", "1|0")
	md := testBoard(t, []Results{evil}, testCorpus).Markdown()
	if strings.Contains(md, "\n## injected") {
		t.Fatalf("a results file injected a heading:\n%s", md)
	}
	if want := `evil\|engine ## injected 1\|0`; strings.Count(md, want) != 3 {
		t.Fatalf("want %q in the facts line, the ranking and the tier table:\n%s", want, md)
	}
}
