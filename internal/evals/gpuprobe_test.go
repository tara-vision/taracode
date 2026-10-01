package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func TestParseProbeAddsUpTheLines(t *testing.T) {
	cases := []struct {
		name, out string
		want      int64
		ok        bool
	}{
		{"one number", "23676\n", 23676, true},
		{"one line per process", "20000\n3676\n", 23676, true},
		{"the unit and blank lines", " 23676 MiB \n\n", 23676, true},
		{"windows line endings", "20000\r\n3676\r\n", 23676, true},
		{"a line that is only the unit", "MiB\n23676\n", 0, false},
		{"nothing", "", 0, false},
		{"only blank lines", "\n\n", 0, false},
		{"zero", "0\n", 0, false},
		{"text", "N/A\n", 0, false},
		{"negative", "-5\n", 0, false},
		{"a decimal", "23676.5\n", 0, false},
		{"another unit", "23 GiB\n", 0, false},
		{"more than any GPU holds", "99999999999\n", 0, false},
		{"a sum past the limit", "900000\n900000\n", 0, false},
		{"the header left on", "memory.used [MiB]\n23676 MiB\n", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseProbe(c.out)
			if (err == nil) != c.ok || got != c.want {
				t.Fatalf("parseProbe(%q) = %d, %v; want %d, ok=%v", c.out, got, err, c.want, c.ok)
			}
		})
	}
}

func TestProbeGPURunsTheCommandInTheGivenEnvironment(t *testing.T) {
	env := []string{"PROBE_VALUE=1234", "PATH=" + os.Getenv("PATH")}
	got, err := probeGPU(context.Background(), `echo "$PROBE_VALUE"`, env)
	if err != nil || got != 1234 {
		t.Fatalf("probeGPU = %d, %v; want 1234", got, err)
	}
}

func TestProbeGPUReportsAFailingCommand(t *testing.T) {
	env := []string{"PATH=" + os.Getenv("PATH")}
	for name, command := range map[string]string{
		"a non-zero exit":             "echo 100; exit 3",
		"no such command":             "definitely-not-a-command-taracode",
		"output that is not a number": "echo not-a-number",
	} {
		if got, err := probeGPU(context.Background(), command, env); err == nil || got != 0 {
			t.Errorf("%s: probeGPU = %d, %v; want an error and 0", name, got, err)
		}
	}
}

func TestProbeGPUGivesUpOnACommandThatHangs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := probeGPU(ctx, "sleep 30; echo 5", []string{"PATH=" + os.Getenv("PATH")})
	if err == nil || got != 0 {
		t.Fatalf("probeGPU = %d, %v; want an error", got, err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the probe took %s to give up", elapsed)
	}
}

func TestProbeGPURefusesAFloodOfOutput(t *testing.T) {
	got, err := probeGPU(context.Background(), "yes 1 | head -c 200000", []string{"PATH=" + os.Getenv("PATH")})
	if err == nil || got != 0 || !strings.Contains(err.Error(), "too much") {
		t.Fatalf("probeGPU = %d, %v; want a too-much-output error", got, err)
	}
}

// loadedGemma is an engine that reports gemma4:12b as loaded, entirely on the GPU, at 9.3 GB.
func loadedGemma(t *testing.T) *ollamatest.Server {
	t.Helper()
	srv := fakeOllama(t, ollamatest.Turn{Content: oomAnswer})
	srv.Loaded = []ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768,
		Size: 9_300_000_000, SizeVRAM: 9_300_000_000, Quantization: "Q4_K_M"}}
	return srv
}

// TestRunRecordsTheMeasuredGPUMemory: the probe runs once the model is loaded, in the operator's own
// environment (not the isolated one the tasks get), and what it prints is the results' memory.
func TestRunRecordsTheMeasuredGPUMemory(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	t.Setenv("HOME", "/operator/home")
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	opts.GPUProbe = `test "$HOME" = /operator/home && echo 23676`
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GPUMemoryMiB != 23676 {
		t.Fatalf("GPUMemoryMiB = %d, want 23676: %s", res.GPUMemoryMiB, out.String())
	}
	if want := ", 23.1 GiB on the GPU (engine reports 9.3 GB, 100% GPU)"; !strings.Contains(out.String(), want) {
		t.Fatalf("the last line lacks %q: %q", want, out.String())
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"gpu_memory_mib":23676`) {
		t.Fatalf("the results lack the measured memory: %s", data)
	}
}

// TestRunSurvivesAFailingProbe: the measurement is metadata. A probe that fails costs one warning on
// the terminal, never the run, and leaves no key in the results.
func TestRunSurvivesAFailingProbe(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	opts.GPUProbe = "echo /secret/path/on/the/host >&2; exit 1"
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GPUMemoryMiB != 0 || len(res.Tasks) != len(tasks) {
		t.Fatalf("GPUMemoryMiB = %d, tasks = %d", res.GPUMemoryMiB, len(res.Tasks))
	}
	if !strings.Contains(out.String(), "warning: no measured GPU memory in the results") {
		t.Fatalf("no warning: %q", out.String())
	}
	if strings.Contains(out.String(), "/secret/path") {
		t.Fatalf("the terminal repeats the probe's stderr: %q", out.String())
	}
	if !strings.Contains(out.String(), ", 9.3 GB (100% GPU)") {
		t.Fatalf("without a measurement the last line keeps the engine's figure: %q", out.String())
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "gpu_memory_mib") || strings.Contains(string(data), "/secret/path") {
		t.Fatalf("the results carry what they must not: %s", data)
	}
}

func TestRunWithoutAProbeMeasuresNothing(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(res)
	if res.GPUMemoryMiB != 0 || strings.Contains(string(data), "gpu_memory_mib") || strings.Contains(out.String(), "warning") {
		t.Fatalf("GPUMemoryMiB = %d, output %q, results %s", res.GPUMemoryMiB, out.String(), data)
	}
}

// TestBoardPrintsMeasuredMemoryAndMarksEstimates: the VRAM column is the measured figure in GiB (the
// unit a card's size is given in); a row that only has the engine's own estimate is marked with "~",
// and a row with neither has a dash.
func TestBoardPrintsMeasuredMemoryAndMarksEstimates(t *testing.T) {
	measured := hardwareResult("glm-4.7-flash", "32", 0.94, 0.96, 140, testEngine(20_720_000_000, 20_720_000_000, 100))
	measured.GPUMemoryMiB = 23676
	estimated := hardwareResult("gemma4:12b", "16", 0.82, 0.88, 95, testEngine(9_300_000_000, 9_300_000_000, 100))
	neither := hardwareResult("unknown-engine:1b", "other", 0.5, 0.5, 0, nil)
	sb := testBoard(t, []Results{measured, estimated, neither}, testCorpus)

	if got := sb.Boards[0].Rows[0].GPUMemoryMiB; got != 23676 {
		t.Fatalf("the row's measured memory is %d", got)
	}
	md := sb.Markdown()
	for _, want := range []string{
		"| 1 | glm-4.7-flash | 32 GB | 94% | 0.96 | 140 | 5 s | 23.1 GiB | Q4_K_M | 1 |",
		"| 2 | gemma4:12b | 16 GB | 82% | 0.88 | 95 | 5 s | ~8.7 GiB | Q4_K_M | 1 |",
		"| 3 | unknown-engine:1b | - | 50% | 0.50 | - | 5 s | - | - | 1 |",
		"VRAM is the GPU memory in use with the model loaded, measured on the machine",
		"A figure marked ~ is the engine's own estimate",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	data, err := json.Marshal(sb)
	if err != nil {
		t.Fatal(err)
	}
	// The measured row is in the board and in its tier table; no other row carries the key.
	if strings.Count(string(data), `"gpu_memory_mib":23676`) != 2 || strings.Count(string(data), "gpu_memory_mib") != 2 {
		t.Fatalf("the scoreboard JSON carries the measured memory for the measured row only: %s", data)
	}
}

// TestBoardSaysWhenNothingWasMeasured: a board with no measured row does not claim a measurement.
func TestBoardSaysWhenNothingWasMeasured(t *testing.T) {
	md := testBoard(t, hardwareResults(), testCorpus).Markdown()
	if strings.Contains(md, "measured on the machine") {
		t.Errorf("a board with no measured row claims a measurement:\n%s", md)
	}
	if want := "VRAM is the engine's own estimate (~), not a measurement"; !strings.Contains(md, want) {
		t.Errorf("missing %q in:\n%s", want, md)
	}
}

// TestParseProbeNeverRepeatsWhatTheProbePrinted: a probe's output can name a host or a path (an ssh
// error sent to stdout, a remote shell's greeting), so the error says which line is wrong and never
// what it holds.
func TestParseProbeNeverRepeatsWhatTheProbePrinted(t *testing.T) {
	_, err := parseProbe("23676\nssh: connect to host gpu-01.corp.example port 22: refused /home/op/.ssh\n")
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
	for _, leak := range []string{"gpu-01", "corp.example", "/home/op", "ssh"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("the error repeats the probe's output (%q): %v", leak, err)
		}
	}
	if _, err := parseProbe(""); err == nil || !strings.Contains(err.Error(), "printed nothing") {
		t.Fatalf("an empty output: %v", err)
	}
}

// TestRunMeasuresOnlyAModelTheEngineListsAsLoaded: when the engine does not describe the model as
// loaded (nothing is listed, or it is listed under another name), what the GPU holds cannot be tied
// to the model, so nothing is recorded and the probe is not run. The warning says what to do about a
// name the engine does not know, and never calls the run's own model "another model".
func TestRunMeasuresOnlyAModelTheEngineListsAsLoaded(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	for name, loaded := range map[string][]ollamatest.LoadedSpec{
		"nothing is listed":         nil,
		"listed under another name": {{Name: "library/gemma4:12b", Size: 9_300_000_000, SizeVRAM: 9_300_000_000}},
	} {
		srv := fakeOllama(t, ollamatest.Turn{Content: oomAnswer})
		srv.Loaded = loaded
		marker := filepath.Join(t.TempDir(), "probed")
		var out bytes.Buffer
		opts := runOptions(srv, "")
		opts.Out = &out
		opts.GPUProbe = `: > "` + marker + `"; echo 23676`
		res, err := Run(context.Background(), tasks, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.Engine != nil || res.GPUMemoryMiB != 0 {
			t.Errorf("%s: engine %+v, measured %d", name, res.Engine, res.GPUMemoryMiB)
		}
		if _, statErr := os.Stat(marker); statErr == nil {
			t.Errorf("%s: the probe ran", name)
		}
		for _, want := range []string{
			"warning: no engine block in the results: the engine does not list gemma4:12b as loaded",
			"use the one `ollama list` prints",
			"warning: no measured GPU memory in the results: the engine did not describe gemma4:12b as loaded",
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: missing %q in %q", name, want, out.String())
			}
		}
		if strings.Contains(out.String(), "other model") {
			t.Errorf("%s: the run's own model is called another model: %q", name, out.String())
		}
	}
}

// TestSpeedAndMemoryNamesAMeasurementAlone: a results file can hold a measurement and no engine
// block (an older run, a hand-kept file); the last line then names the measurement alone.
func TestSpeedAndMemoryNamesAMeasurementAlone(t *testing.T) {
	got := speedAndMemory(Results{GPUMemoryMiB: 23676, Summary: Summary{TokensPerS: 100}})
	if got != ", 100 tok/s, 23.1 GiB on the GPU" {
		t.Fatalf("speedAndMemory = %q", got)
	}
}

// TestAModelOnTheCPUOnlyIsNotInTheGPUsFigure: another model the engine holds entirely in system
// memory (an embedding model, say) takes nothing on the GPU, so it does not stop the measurement.
func TestAModelOnTheCPUOnlyIsNotInTheGPUsFigure(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	srv.Loaded = append(srv.Loaded, ollamatest.LoadedSpec{Name: "embed:1b", Size: 600_000_000, SizeVRAM: 0})
	opts := runOptions(srv, "")
	opts.GPUProbe = "echo 23676"
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GPUMemoryMiB != 23676 {
		t.Fatalf("measured %d, want 23676", res.GPUMemoryMiB)
	}
}

// TestBoardWithEveryRowMeasuredSaysOnlyThat: no "~" sentence when there is no estimate to mark.
func TestBoardWithEveryRowMeasuredSaysOnlyThat(t *testing.T) {
	a := hardwareResult("glm-4.7-flash", "32", 0.94, 0.96, 140, testEngine(20_720_000_000, 20_720_000_000, 100))
	a.GPUMemoryMiB = 23676
	b := hardwareResult("gemma4:12b", "16", 0.82, 0.88, 95, nil) // measured, although the engine described nothing
	b.GPUMemoryMiB = 9500
	md := testBoard(t, []Results{a, b}, testCorpus).Markdown()
	for _, want := range []string{
		"VRAM is the GPU memory in use with the model loaded, measured on the machine",
		"| 2 | gemma4:12b | 16 GB | 82% | 0.88 | 95 | 5 s | 9.3 GiB | - | 1 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "marked ~") || strings.Contains(md, "~") {
		t.Errorf("a board with every row measured still talks about estimates:\n%s", md)
	}
}

// TestRunDoesNotRepeatAProbesOutputOnTheTerminal: what a probe prints instead of a number may name a
// host; the warning says which line is wrong, not what it holds.
func TestRunDoesNotRepeatAProbesOutputOnTheTerminal(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	opts.GPUProbe = "echo connect to host gpu-01.corp.example refused"
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warning: no measured GPU memory") || strings.Contains(out.String(), "gpu-01") {
		t.Fatalf("output %q", out.String())
	}
}

// TestRunDoesNotMeasureWhileAnotherModelIsLoaded: the GPU's figure would include the other model, so
// the run says so and records no measurement; the probe is not even run.
func TestRunDoesNotMeasureWhileAnotherModelIsLoaded(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	srv.Loaded = append(srv.Loaded, ollamatest.LoadedSpec{Name: "other:7b", Size: 5_000_000_000, SizeVRAM: 5_000_000_000})
	marker := filepath.Join(t.TempDir(), "probed")
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	opts.GPUProbe = `: > "` + marker + `"; echo 23676`
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GPUMemoryMiB != 0 || res.Engine == nil {
		t.Fatalf("measured %d, engine %+v", res.GPUMemoryMiB, res.Engine)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("the probe ran although another model was loaded")
	}
	want := "warning: no measured GPU memory in the results: 1 other model is loaded on the engine"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("missing %q in %q", want, out.String())
	}
}

// TestAProbedRunUnloadsItsModelWhenItEnds: a model left on the GPU would be inside the next run's
// measurement, so a run that measures cleans up after itself; a run without a probe leaves the
// engine as it is.
func TestAProbedRunUnloadsItsModelWhenItEnds(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	plain := loadedGemma(t)
	if _, err := Run(context.Background(), tasks, runOptions(plain, "")); err != nil {
		t.Fatal(err)
	}
	if len(plain.Unloaded) != 0 {
		t.Fatalf("a run without a probe unloaded %v", plain.Unloaded)
	}
	probed := loadedGemma(t)
	opts := runOptions(probed, "")
	opts.GPUProbe = "echo 23676"
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	if len(probed.Unloaded) != 1 || probed.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("a probed run unloaded %v, want its own model once", probed.Unloaded)
	}
}

// TestRunRefusesAMeasurementFarBelowTheEnginesEstimate: a probe that asks the wrong machine, the
// wrong GPU or prints another unit gives a figure far below even the engine's own estimate. It is
// not recorded as a measurement.
func TestRunRefusesAMeasurementFarBelowTheEnginesEstimate(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	for probe, measured := range map[string]int64{"echo 23": 0, "echo 4400": 0, "echo 4500": 4500, "echo 23676": 23676} {
		srv := loadedGemma(t) // the engine estimates 9.3 GB, about 8869 MiB
		var out bytes.Buffer
		opts := runOptions(srv, "")
		opts.Out = &out
		opts.GPUProbe = probe
		res, err := Run(context.Background(), tasks, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.GPUMemoryMiB != measured {
			t.Errorf("%s: recorded %d, want %d", probe, res.GPUMemoryMiB, measured)
		}
		if warned := strings.Contains(out.String(), "less than half of the engine's own estimate"); warned != (measured == 0) {
			t.Errorf("%s: warned=%v: %q", probe, warned, out.String())
		}
	}
}

// TestMeasuredGPUIsSilentWhenTheRunIsAlreadyCancelled: a cancelled run is not a probe that timed out.
func TestMeasuredGPUIsSilentWhenTheRunIsAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	got := measuredGPU(ctx, RunOptions{GPUProbe: "echo 5", Out: &out}, onGPU(), 0)
	if got != 0 || out.Len() != 0 {
		t.Fatalf("measuredGPU = %d, output %q", got, out.String())
	}
}

// TestMeasuredGPUWorksWithoutARunScope: outside Run the probe gets the process environment.
func TestMeasuredGPUWorksWithoutARunScope(t *testing.T) {
	t.Setenv("PROBE_VALUE", "777")
	var out bytes.Buffer
	opts := RunOptions{GPUProbe: `echo "$PROBE_VALUE"`, Out: &out}
	if got := measuredGPU(context.Background(), opts, &EngineInfo{VRAMBytes: 1 << 20}, 0); got != 777 {
		t.Fatalf("measuredGPU = %d, output %q", got, out.String())
	}
}

// TestBoardWithNoMemoryFigureAtAllSaysSo: nothing measured and nothing reported is not "an estimate".
func TestBoardWithNoMemoryFigureAtAllSaysSo(t *testing.T) {
	md := testBoard(t, []Results{hardwareResult("unknown-engine:1b", "other", 0.5, 0.5, 0, nil)}, testCorpus).Markdown()
	if strings.Contains(md, "estimate") || !strings.Contains(md, "No run on this board measured or reported its memory.") {
		t.Errorf("the note for a board with no memory figure:\n%s", md)
	}
}

// TestUnloadAfterIsBestEffort: with no engine to ask (no host), the clean-up gives up at once and
// quietly; with one, it asks for the run's own model and nothing else.
func TestUnloadAfterIsBestEffort(t *testing.T) {
	start := time.Now()
	unloadAfter(context.Background(), RunOptions{Model: "gemma4:12b"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("giving up took %s", elapsed)
	}
	srv := loadedGemma(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the run's own context is done; the clean-up still happens
	unloadAfter(ctx, runOptions(srv, ""))
	if len(srv.Unloaded) != 1 || srv.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("unloaded %v", srv.Unloaded)
	}
}
