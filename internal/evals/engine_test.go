package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// timed is a scripted reply with the engine timings the speed numbers come from.
func timed(turn taraenginetest.Turn, promptMs, evalMs int) taraenginetest.Turn {
	turn.PromptEvalDuration = time.Duration(promptMs) * time.Millisecond
	turn.EvalDuration = time.Duration(evalMs) * time.Millisecond
	return turn
}

const oomAnswer = "OOMKilled at the memory limit."

func TestRunRecordsTheEngineTimingsAndTheGenerationRate(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	describe := call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"})
	// call() answers 100 prompt and 10 completion tokens; the final answer adds 400 and 30.
	srv := fakeEngine(t,
		timed(describe, 200, 100),
		timed(taraenginetest.Turn{Content: oomAnswer, PromptTokens: 400, CompletionTokens: 30}, 300, 300),
	)
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if tr := res.Tasks[0]; tr.PromptEvalMs != 500 || tr.EvalMs != 400 || tr.CompletionTokens != 40 {
		t.Fatalf("task %+v", tr)
	}
	if res.Summary.TokensPerS != 100 { // 40 tokens in 0.4 s
		t.Fatalf("summary %+v", res.Summary)
	}
	if !strings.Contains(out.String(), ", 100 tok/s") {
		t.Fatalf("the last line lacks the rate: %q", out.String())
	}
}

func TestRunAveragesTheEngineTimingsOverRuns(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	answer := taraenginetest.Turn{Content: oomAnswer, PromptTokens: 40, CompletionTokens: 20}
	opts := runOptions(fakeEngine(t, timed(answer, 100, 200), timed(answer, 300, 600)), "")
	opts.Runs = 2
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if tr := res.Tasks[0]; tr.PromptEvalMs != 200 || tr.EvalMs != 400 || tr.CompletionTokens != 20 {
		t.Fatalf("task %+v", tr)
	}
	if res.Summary.TokensPerS != 50 { // 20 tokens a run in a mean of 0.4 s
		t.Fatalf("summary %+v", res.Summary)
	}
}

// TestRunWritesNoSpeedOrEngineKeysWhenTheEngineReportsNone pins the old file shape: an engine with no
// timings and no loaded-model entry, and a run with no hardware label, write none of the new keys.
func TestRunWritesNoSpeedOrEngineKeysWhenTheEngineReportsNone(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, taraenginetest.Turn{Content: oomAnswer, PromptTokens: 40, CompletionTokens: 6})
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tokens_per_s", "eval_ms", "prompt_eval_ms", "hardware", "engine", "gpu_memory_mib"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("%s is written although the run has none: %s", key, data)
		}
	}
}

func TestRunCapturesTheLoadedModel(t *testing.T) {
	cases := []struct {
		name    string
		loaded  []taraenginetest.LoadedSpec
		detail  taraenginetest.ModelSpec // the quantization, parameter size and digest the engine shows for the model
		want    *EngineInfo
		warning bool
	}{
		{"entirely on the GPU",
			[]taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000}},
			taraenginetest.ModelSpec{Quantization: "Q4_K_M", ParameterSize: "11.9B", Digest: "sha256:0123456789abcdef0123"},
			&EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768,
				Quantization: "Q4_K_M", ParameterSize: "11.9B", Digest: "0123456789ab", Measured: true}, false},
		{"estimated by the engine",
			[]taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000,
				Source: "estimated"}},
			taraenginetest.ModelSpec{},
			&EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768}, false},
		{"odd engine strings are dropped, not published",
			[]taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000}},
			taraenginetest.ModelSpec{Quantization: "Q4 | at http://10.1.2.3:11434", ParameterSize: "11.9B",
				Digest: "sha256:not-hex-at-all"},
			&EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768,
				ParameterSize: "11.9B", Measured: true}, false},
		{"another model is loaded", []taraenginetest.LoadedSpec{{Name: "other:1b", Size: 1}}, taraenginetest.ModelSpec{},
			nil, true},
		{"the engine reports no size", []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768}},
			taraenginetest.ModelSpec{}, nil, true},
		{"nothing is loaded", nil, taraenginetest.ModelSpec{}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tasks := corpusWithTriage(t)
			srv := fakeEngine(t, taraenginetest.Turn{Content: oomAnswer})
			srv.Loaded = c.loaded
			srv.Models[0].Quantization, srv.Models[0].ParameterSize, srv.Models[0].Digest =
				c.detail.Quantization, c.detail.ParameterSize, c.detail.Digest
			var out bytes.Buffer
			opts := runOptions(srv, "")
			opts.Out = &out
			res, err := Run(context.Background(), tasks, opts)
			if err != nil {
				t.Fatal(err)
			}
			if (res.Engine == nil) != (c.want == nil) || (c.want != nil && *res.Engine != *c.want) {
				t.Fatalf("engine %+v, want %+v", res.Engine, c.want)
			}
			if warned := strings.Contains(out.String(), "warning: no engine block"); warned != c.warning {
				t.Fatalf("warning=%v, want %v: %q", warned, c.warning, out.String())
			}
			if c.want != nil { // the run's last line says what the model takes and where it sits
				memory := fmt.Sprintf(", %.1f GB (%d%% GPU)", float64(c.want.SizeBytes)/1e9, c.want.GPUPercent)
				if c.want.Measured { // the run records the engine's measurement, and the line names it as such
					memory = fmt.Sprintf(", %.1f GiB on the GPU, measured by the engine", gib(c.want.VRAMBytes>>20))
				}
				if !strings.Contains(out.String(), memory) {
					t.Fatalf("the last line lacks %q: %q", memory, out.String())
				}
			}
		})
	}
}

// TestRunSurvivesAFailingEngineCall: when the engine cannot describe its loaded models, the run goes
// on without the block, warns once on the terminal, and writes nothing of the engine's error into
// the results.
func TestRunSurvivesAFailingEngineCall(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, taraenginetest.Turn{Content: oomAnswer})
	// The error names an address and a home path on purpose: a caller must publish neither.
	srv.Failures["GET /api/loaded"] = &taraenginetest.Failure{Status: http.StatusInternalServerError,
		Message: "loaded failed at http://10.1.2.3:11434 reading /home/ollama/.ollama"}
	var out bytes.Buffer
	opts := runOptions(srv, "")
	opts.Out = &out
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	// The task still ran to its end and was scored: the failed engine call cost nothing but the block.
	if res.Engine != nil || strings.Count(out.String(), "warning: no engine block") != 1 ||
		len(res.Tasks) != 1 || res.Tasks[0].Error != "" || res.Tasks[0].Answer != 1 {
		t.Fatalf("engine %+v, output %q, tasks %+v", res.Engine, out.String(), res.Tasks)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"10.1.2.3", "/home/ollama", srv.URL} {
		if strings.Contains(string(data), leak) {
			t.Errorf("the results carry %q from the engine's error: %s", leak, data)
		}
	}
}

// TestGenerationRateSkipsTasksWithNoEngineTime: tokens of a task the engine timed nothing for must
// not be divided by the other tasks' time.
func TestGenerationRateSkipsTasksWithNoEngineTime(t *testing.T) {
	rows := []TaskResult{{CompletionTokens: 1000, EvalMs: 10_000}, {CompletionTokens: 1000}}
	if got := generationRate(rows); got != 100 {
		t.Fatalf("rate %v, want 100", got)
	}
	if got := generationRate([]TaskResult{{CompletionTokens: 1000}}); got != 0 {
		t.Fatalf("rate %v with no timed task, want 0", got)
	}
}

func TestRunWritesTheHardwareLabel(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	opts := runOptions(fakeEngine(t, taraenginetest.Turn{Content: oomAnswer}), "")
	opts.Hardware = "NVIDIA RTX 5090 (32 GB)"
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hardware != "NVIDIA RTX 5090 (32 GB)" || res.Host != "lab" {
		t.Fatalf("header %+v", res)
	}
}

func TestGPUPercentIsAHundredOnlyWhenEverythingIsOnTheGPU(t *testing.T) {
	cases := []struct {
		size, vram int64
		want       int
	}{
		{20_720_000_000, 20_720_000_000, 100},
		{20_720_000_000, 20_719_999_999, 99},
		{1000, 996, 99},
		{40_000_000_000, 30_000_000_000, 75},
		{10, 0, 0},
		{10, -3, 0},
		{10, 11, 100},
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := gpuPercent(c.size, c.vram); got != c.want {
			t.Errorf("gpuPercent(%d, %d) = %d, want %d", c.size, c.vram, got, c.want)
		}
	}
}

func TestCleanHardware(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"  NVIDIA RTX 5090 (32 GB) ", "NVIDIA RTX 5090 (32 GB)", false},
		{"", "", false},
		{"Apple M4 Max, 64 GB", "Apple M4 Max, 64 GB", false},
		{"NVIDIA   RTX  5090", "NVIDIA RTX 5090", false}, // inner runs of spaces collapse, so one machine is one board
		{"RTX\xff5090", "", true},                        // not valid UTF-8
		{"http://192.0.2.10:11434", "", true},            // a URL is an address, not a label
		{"the box at 192.0.2.10", "", true},              // so is a bare IP address
		{"gpu://somewhere", "", true},
		{strings.Repeat("x", 60), strings.Repeat("x", 60), false},
		{strings.Repeat("x", 61), "", true},
		{"two\nlines", "", true},
		{"tab\there", "", true},
		{"bell\ahere", "", true},
	}
	for _, c := range cases {
		got, err := CleanHardware(c.in)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("CleanHardware(%q) = %q, %v; want %q, error %v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestShortDigest(t *testing.T) {
	cases := map[string]string{
		"sha256:0123456789abcdef0123": "0123456789ab", // cut to twelve
		"4475827791A2":                "4475827791A2",
		"sha256:abc123":               "abc123", // shorter than twelve stays whole
		"sha256:not-hex":              "",
		"":                            "",
	}
	for in, want := range cases {
		if got := shortDigest(in); got != want {
			t.Errorf("shortDigest(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEngineInfoLeavesTheDescriptionsOutWhenTheDetailFails: the quantization, the parameter size and the digest
// come from the engine's detail of the model; when it cannot show one, the block keeps what /api/loaded gave.
func TestEngineInfoLeavesTheDescriptionsOutWhenTheDetailFails(t *testing.T) {
	srv := fakeEngine(t)
	srv.Models[0].Quantization, srv.Models[0].ParameterSize, srv.Models[0].Digest = "Q4_K_M", "12B", "a1b2c3d4e5f6"
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000}}
	srv.Failures["GET /api/models/gemma4:12b"] = &taraenginetest.Failure{Status: http.StatusInternalServerError,
		Message: "broken"}
	info, _, err := engineInfo(context.Background(), withDefaults(runOptions(srv, "")))
	want := EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768,
		Measured: true}
	if err != nil || info == nil || *info != want {
		t.Fatalf("info %+v, err %v", info, err)
	}
}

// TestEngineInfoReadsTheEngine: the entry named as the model was asked for is the block, with the engine's
// measurement and its detail; another loaded model with memory on the GPU counts as one other.
func TestEngineInfoReadsTheEngine(t *testing.T) {
	srv := fakeEngine(t)
	srv.Models[0].Quantization, srv.Models[0].ParameterSize, srv.Models[0].Digest = "Q4_K_M", "12B", "a1b2c3d4e5f6"
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000},
		{Name: "other:1b", Size: 1_000_000_000}}
	info, others, err := engineInfo(context.Background(), withDefaults(runOptions(srv, "")))
	want := EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768,
		Quantization: "Q4_K_M", ParameterSize: "12B", Digest: "a1b2c3d4e5f6", Measured: true}
	if err != nil || info == nil || *info != want || others != 1 {
		t.Fatalf("info %+v, others %d, err %v", info, others, err)
	}
}

// TestEngineInfoWithoutAFigure: a model the engine lists as loaded, with no memory figure, gives no block.
func TestEngineInfoWithoutAFigure(t *testing.T) {
	srv := fakeEngine(t)
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Source: "none"}}
	if _, _, err := engineInfo(context.Background(), withDefaults(runOptions(srv, ""))); err == nil ||
		!strings.Contains(err.Error(), "the engine reports no memory for gemma4:12b") {
		t.Fatalf("no memory figure: %v", err)
	}
}

// TestEngineInfoFailsWithoutAHost: the capture reports why it has nothing, it never panics.
func TestEngineInfoFailsWithoutAHost(t *testing.T) {
	info, _, err := engineInfo(context.Background(), RunOptions{Model: "gemma4:12b"})
	if info != nil || err == nil {
		t.Fatalf("info %+v, err %v", info, err)
	}
}
