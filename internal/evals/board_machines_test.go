package evals

import (
	"fmt"
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
		{"(Mac) M4", "gemma4-31b-taraengine-mac-m4-2026-10-17"},
		{"", "gemma4-31b-taraengine-2026-10-17"},
		// A label with no ASCII letter or digit is named by the first 8 hex digits of its sha256, and one that loses a
		// letter or digit outside ASCII adds them, so two such machines still keep a file each.
		{"Мак Студио", "gemma4-31b-taraengine-machine-6d813104-2026-10-17"},
		{"(!)", "gemma4-31b-taraengine-machine-09587ee8-2026-10-17"},
		{"Мак Studio", "gemma4-31b-taraengine-studio-3cd91284-2026-10-17"},
		{"ПК Studio", "gemma4-31b-taraengine-studio-97047fee-2026-10-17"},
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
// tier table lists it once per machine, naming the machine (a dash for a run without a label). Rows that tie on
// score fall back to the machine's name, on every build.
func TestScoreboardKeepsAModelOnEachMachine(t *testing.T) {
	results := []Results{
		onMachine("gemma4:12b", "16", testHardware, true, 0.88, 308),
		onMachine("gemma4:12b", "16", macHardware, true, 0.88, 83),
		onMachine("qwen3.6:27b", "16", "", false, 0.5, 0),
	}
	sb := testBoard(t, results, testCorpus)
	if len(sb.Boards) != 2 {
		t.Fatalf("%d boards, want one per machine", len(sb.Boards))
	}
	for _, b := range sb.Boards {
		if got := boardModels(b.Rows); !slices.Equal(got, []string{"gemma4:12b"}) {
			t.Fatalf("%s ranks %v", b.Hardware, got)
		}
	}
	for range 20 {
		rows := testBoard(t, results, testCorpus).Tiers[0].Rows
		if len(rows) != 3 || rows[0].Hardware != macHardware || rows[1].Hardware != testHardware || rows[2].Hardware != "" {
			t.Fatalf("tier rows %+v", rows)
		}
	}
	md := sb.Markdown()
	for _, want := range []string{
		"\n| Model | Machine | Pass rate | Mean score | kubernetes | helm | terraform | docker | secrets | cloud | refusal | " +
			"Mean iterations | Mean wall | Misses | Engine | Date |\n" + "|" + strings.Repeat("---|", 16) + "\n",
		"\n| gemma4:12b (default) | Apple M4 Max (48 GB) | 88% |",
		"\n| gemma4:12b (default) | NVIDIA RTX 5090 (32 GB) | 88% |",
		"\n| qwen3.6:27b | - | 50% |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the tier table lacks %q:\n%s", want, md)
		}
	}
}

// TestEachMachineKeepsItsOwnDefault: a run's default flag comes from the engine catalog of that run, so two machines
// can disagree (one board run before a catalog change); every row carries its own machine's flag.
func TestEachMachineKeepsItsOwnDefault(t *testing.T) {
	sb := testBoard(t, []Results{
		onMachine("qwen3.6:35b", "48", testHardware, true, 0.82, 388),
		onMachine("gemma4:31b", "48", testHardware, false, 0.91, 164),
		onMachine("qwen3.6:35b", "48", macHardware, false, 0.8, 60),
		onMachine("gemma4:31b", "48", macHardware, true, 0.9, 20),
	}, testCorpus)
	var flags []string
	for _, r := range sb.Tiers[0].Rows {
		flags = append(flags, fmt.Sprintf("%s %s %v", r.Model, r.Hardware, r.Default))
	}
	want := []string{"gemma4:31b " + testHardware + " false", "gemma4:31b " + macHardware + " true",
		"qwen3.6:35b " + testHardware + " true", "qwen3.6:35b " + macHardware + " false"}
	if !slices.Equal(flags, want) {
		t.Fatalf("default flags %q\nwant %q", flags, want)
	}
	checks := []string{
		"48 on Apple M4 Max (48 GB): default gemma4:31b, top scorer gemma4:31b",
		"48 on NVIDIA RTX 5090 (32 GB): default qwen3.6:35b, top scorer gemma4:31b (differs)",
	}
	if got := sb.CheckDefaults(); !slices.Equal(got, checks) {
		t.Fatalf("checks %q\nwant %q", got, checks)
	}
}

// TestSkippedNamesTheMachine: a run left out for a safety failure is named with its machine, so two machines' runs
// of one model read apart.
func TestSkippedNamesTheMachine(t *testing.T) {
	failed := func(hw string) Results {
		r := onMachine("gemma4:12b", "16", hw, true, 0.88, 100)
		r.Summary.SafetyFailures = 1
		return r
	}
	sb := testBoard(t, []Results{failed(testHardware), failed(macHardware)}, testCorpus)
	want := []string{
		"gemma4:12b on taraengine 0.3.1, Apple M4 Max (48 GB) 2026-10-17: 1 safety failure(s)",
		"gemma4:12b on taraengine 0.3.1, NVIDIA RTX 5090 (32 GB) 2026-10-17: 1 safety failure(s)",
	}
	if !slices.Equal(sb.Skipped, want) {
		t.Fatalf("skipped %q\nwant %q", sb.Skipped, want)
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
	results := []Results{
		onMachine("gemma4:12b", "16", "", true, 0.5, 0),
		onMachine("gemma4:12b", "16", macHardware, true, 0.88, 83),
	}
	for range 20 { // the rows sit in a map: every build has to agree
		sb := testBoard(t, results, testCorpus)
		if rows := sb.Tiers[0].Rows; len(rows) != 1 || rows[0].Hardware != macHardware || len(sb.Boards) != 1 {
			t.Fatalf("rows %+v, %d boards", rows, len(sb.Boards))
		}
	}
}

// TestOneMachineTierTablesNameNoMachine: a board from one machine, with an unlabelled run beside it, keeps the tier
// tables it had.
func TestOneMachineTierTablesNameNoMachine(t *testing.T) {
	sb := testBoard(t, hardwareResults(), testCorpus)
	md := sb.Markdown()
	if strings.Contains(md, "Machine") || !strings.Contains(md, "| Model | Pass rate | Mean score |") {
		t.Fatalf("a one-machine board changed its tier tables:\n%s", md)
	}
	for _, line := range sb.CheckDefaults() {
		if strings.Contains(line, " on ") || strings.Contains(line, "machine") {
			t.Errorf("a one-machine board's check names a machine: %q", line)
		}
	}
}

// TestCheckDefaultsChecksUnlabelledRowsOnAManyMachineBoard: with two machines on the board, a tier's rows that name
// no machine get a line of their own.
func TestCheckDefaultsChecksUnlabelledRowsOnAManyMachineBoard(t *testing.T) {
	sb := testBoard(t, []Results{
		onMachine("gemma4:12b", "16", testHardware, true, 0.88, 308),
		onMachine("gemma4:12b", "16", macHardware, true, 0.88, 83),
		onMachine("glm-4.7-flash", "32", "", false, 0.91, 0),
	}, testCorpus)
	want := []string{
		"16 on Apple M4 Max (48 GB): default gemma4:12b, top scorer gemma4:12b",
		"16 on NVIDIA RTX 5090 (32 GB): default gemma4:12b, top scorer gemma4:12b",
		"32 without a machine label: default none, top scorer glm-4.7-flash (differs)",
	}
	if got := sb.CheckDefaults(); !slices.Equal(got, want) {
		t.Fatalf("checks %q\nwant %q", got, want)
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
