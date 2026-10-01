package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
		"a non-zero exit":      "echo 100; exit 3",
		"no such command":      "definitely-not-a-command-taracode",
		"output that is not a": "echo not-a-number",
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

// TestParseProbeClipsALongLineInItsError: the warning names what the probe printed, never at length.
func TestParseProbeClipsALongLineInItsError(t *testing.T) {
	_, err := parseProbe(strings.Repeat("x", 500) + "\n")
	if err == nil || len(err.Error()) > 160 || !strings.Contains(err.Error(), strings.Repeat("x", 40)+"...") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunNamesTheMeasuredMemoryAloneWhenTheEngineDescribesNothing: the probe does not depend on the
// engine's own report.
func TestRunNamesTheMeasuredMemoryAloneWhenTheEngineDescribesNothing(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t, ollamatest.Turn{Content: oomAnswer}) // nothing listed as loaded
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	opts.GPUProbe = "echo 23676"
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Engine != nil || res.GPUMemoryMiB != 23676 {
		t.Fatalf("engine %+v, measured %d", res.Engine, res.GPUMemoryMiB)
	}
	last := out.String()[strings.LastIndex(strings.TrimSpace(out.String()), "\n")+1:]
	if !strings.HasSuffix(strings.TrimSpace(last), ", 23.1 GiB on the GPU") || strings.Contains(last, "engine reports") {
		t.Fatalf("the last line: %q", last)
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
