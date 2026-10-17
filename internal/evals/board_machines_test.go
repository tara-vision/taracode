package evals

import (
	"slices"
	"strings"
	"testing"
)

const macHardware = "Apple M4 Max (48 GB)"

// onMachine is a taraengine run of model on the machine named hw, entirely on its GPU.
func onMachine(model, tier, hw string, isDefault bool, score, tps float64) Results {
	return Results{Taracode: "v3.3.2", Model: model, Tier: tier, Default: isDefault, Think: "auto", Date: "2026-10-17",
		Runs: 1, Hardware: hw, Engine: onGPU(), Server: &ServerInfo{Name: "taraengine", Version: "0.3.1", Runtime: "b11514"},
		Tasks:   []TaskResult{{ID: "a"}, {ID: "b"}},
		Summary: Summary{PassRate: score, MeanScore: score, MeanWallMs: 5000, TokensPerS: tps, ByArea: map[string]AreaSummary{}}}
}

// TestResultsNameKeepsTwoMachinesApart: one model on one server on one day writes a file per machine, and a run
// without a usable machine label keeps the name it had.
func TestResultsNameKeepsTwoMachinesApart(t *testing.T) {
	cases := []struct{ hardware, want string }{
		{testHardware, "gemma4-31b-taraengine-nvidia-rtx-5090-32-gb-2026-10-17"},
		{macHardware, "gemma4-31b-taraengine-apple-m4-max-48-gb-2026-10-17"},
		{"", "gemma4-31b-taraengine-2026-10-17"},
		{"(!)", "gemma4-31b-taraengine-2026-10-17"},
	}
	for _, c := range cases {
		if got := ResultsName(onMachine("gemma4:31b", "48", c.hardware, false, 0.9, 100)); got != c.want {
			t.Errorf("ResultsName with hardware %q = %q, want %q", c.hardware, got, c.want)
		}
	}
	dir := t.TempDir()
	for _, hw := range []string{testHardware, macHardware} {
		if _, err := WriteResults(dir, onMachine("gemma4:31b", "48", hw, false, 0.9, 100)); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := ReadResults(dir); err != nil || len(got) != 2 {
		t.Fatalf("both machines' results must survive in one directory: %d results, %v", len(got), err)
	}
}

// TestScoreboardKeepsAModelOnEachMachine: the same model on the same server ranks on each machine's board, and the
// tier table lists it once per machine, naming the machine.
func TestScoreboardKeepsAModelOnEachMachine(t *testing.T) {
	sb := testBoard(t, []Results{
		onMachine("gemma4:12b", "16", testHardware, true, 0.88, 308),
		onMachine("gemma4:12b", "16", macHardware, true, 0.88, 83),
	}, testCorpus)
	if len(sb.Boards) != 2 {
		t.Fatalf("%d boards, want one per machine", len(sb.Boards))
	}
	for _, b := range sb.Boards {
		if got := boardModels(b.Rows); !slices.Equal(got, []string{"gemma4:12b"}) {
			t.Fatalf("%s ranks %v", b.Hardware, got)
		}
	}
	if len(sb.Tiers) != 1 || len(sb.Tiers[0].Rows) != 2 || sb.Tiers[0].Rows[0].Hardware != macHardware ||
		sb.Tiers[0].Rows[1].Hardware != testHardware {
		t.Fatalf("tiers %+v", sb.Tiers)
	}
	md := sb.Markdown()
	for _, want := range []string{
		"| Model | Machine | Pass rate | Mean score |",
		"| gemma4:12b (default) | Apple M4 Max (48 GB) | 88% |",
		"| gemma4:12b (default) | NVIDIA RTX 5090 (32 GB) | 88% |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the tier table lacks %q:\n%s", want, md)
		}
	}
}

// TestTheNewestLabelledRunKeepsEachMachinesRow: when a model's newest result names a machine, every machine keeps its
// own newest result, older ones included, and an unlabelled run in between drops out; the board text shows each.
func TestTheNewestLabelledRunKeepsEachMachinesRow(t *testing.T) {
	gpu := onMachine("gemma4:31b", "48", testHardware, true, 0.91, 164)
	gpu.Date = "2026-10-15"
	unlabelled := onMachine("gemma4:31b", "48", "", true, 0.5, 0)
	unlabelled.Date = "2026-10-16"
	mac := onMachine("gemma4:31b", "48", macHardware, true, 0.9, 20)
	sb := testBoard(t, []Results{gpu, unlabelled, mac}, testCorpus)
	var machines []string
	for _, r := range sb.Tiers[0].Rows {
		machines = append(machines, r.Hardware+" "+r.Date)
	}
	want := []string{testHardware + " 2026-10-15", macHardware + " 2026-10-17"} // by score: 0.91, then 0.9
	if !slices.Equal(machines, want) || len(sb.Boards) != 2 {
		t.Fatalf("rows %q, %d boards; want %q on two boards", machines, len(sb.Boards), want)
	}
}

// TestOnTheSameDayTheLabelledRunCountsAsNewer: a labelled and an unlabelled run of one model on one day leave the
// labelled row on its board.
func TestOnTheSameDayTheLabelledRunCountsAsNewer(t *testing.T) {
	sb := testBoard(t, []Results{
		onMachine("gemma4:12b", "16", "", true, 0.5, 0),
		onMachine("gemma4:12b", "16", macHardware, true, 0.88, 83),
	}, testCorpus)
	if rows := sb.Tiers[0].Rows; len(rows) != 1 || rows[0].Hardware != macHardware || len(sb.Boards) != 1 {
		t.Fatalf("rows %+v, %d boards", rows, len(sb.Boards))
	}
}

// TestOneMachineTierTablesNameNoMachine: a board from one machine, with an unlabelled run beside it, keeps the tier
// tables it had.
func TestOneMachineTierTablesNameNoMachine(t *testing.T) {
	md := testBoard(t, hardwareResults(), testCorpus).Markdown()
	if strings.Contains(md, "Machine") || !strings.Contains(md, "| Model | Pass rate | Mean score |") {
		t.Fatalf("a one-machine board changed its tier tables:\n%s", md)
	}
}

// TestCheckDefaultsPerMachine: with two machines, each tier's default is checked against each machine's top scorer;
// the same default can lead on one machine and trail on the other, and a machine with no row in a tier has no line.
func TestCheckDefaultsPerMachine(t *testing.T) {
	sb := testBoard(t, []Results{
		onMachine("gemma4:12b", "16", testHardware, true, 0.88, 308),
		onMachine("qwen3.6:27b", "16", testHardware, false, 0.85, 127),
		onMachine("gemma4:12b", "16", macHardware, true, 0.85, 83),
		onMachine("qwen3.6:27b", "16", macHardware, false, 0.88, 40),
		onMachine("glm-4.7-flash", "32", testHardware, false, 0.91, 209),
	}, testCorpus)
	want := []string{
		"16 on Apple M4 Max (48 GB): default gemma4:12b, top scorer qwen3.6:27b (differs)",
		"16 on NVIDIA RTX 5090 (32 GB): default gemma4:12b, top scorer gemma4:12b",
		"32 on NVIDIA RTX 5090 (32 GB): default none, top scorer glm-4.7-flash (differs)",
	}
	if got := sb.CheckDefaults(); !slices.Equal(got, want) {
		t.Fatalf("checks %q\nwant %q", got, want)
	}
}
