package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// timed is a scripted reply with the engine timings the speed numbers come from.
func timed(turn ollamatest.Turn, promptMs, evalMs int) ollamatest.Turn {
	turn.PromptEvalDuration = time.Duration(promptMs) * time.Millisecond
	turn.EvalDuration = time.Duration(evalMs) * time.Millisecond
	return turn
}

const oomAnswer = "OOMKilled at the memory limit."

func TestRunRecordsTheEngineTimingsAndTheGenerationRate(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	describe := call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"})
	// call() answers 100 prompt and 10 completion tokens; the final answer adds 400 and 30.
	srv := fakeOllama(t,
		timed(describe, 200, 100),
		timed(ollamatest.Turn{Content: oomAnswer, PromptTokens: 400, CompletionTokens: 30}, 300, 300),
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
	answer := ollamatest.Turn{Content: oomAnswer, PromptTokens: 40, CompletionTokens: 20}
	opts := runOptions(fakeOllama(t, timed(answer, 100, 200), timed(answer, 300, 600)), "")
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
	srv := fakeOllama(t, ollamatest.Turn{Content: oomAnswer, PromptTokens: 40, CompletionTokens: 6})
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tokens_per_s", "eval_ms", "prompt_eval_ms", "hardware", "engine"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("%s is written although the run has none: %s", key, data)
		}
	}
}

func TestRunCapturesTheLoadedModel(t *testing.T) {
	cases := []struct {
		name    string
		loaded  []ollamatest.LoadedSpec
		want    *EngineInfo
		warning bool
	}{
		{"entirely on the GPU",
			[]ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000, SizeVRAM: 9_300_000_000,
				Quantization: "Q4_K_M", ParameterSize: "11.9B", Digest: "sha256:0123456789abcdef0123"}},
			&EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768,
				Quantization: "Q4_K_M", ParameterSize: "11.9B", Digest: "0123456789ab"}, false},
		{"one byte short of the GPU",
			[]ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 10_000_000_000, SizeVRAM: 9_999_999_999}},
			&EngineInfo{SizeBytes: 10_000_000_000, VRAMBytes: 9_999_999_999, GPUPercent: 99, ContextLength: 32768}, false},
		{"odd engine strings are dropped, not published",
			[]ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000, SizeVRAM: 9_300_000_000,
				Quantization: "Q4 | at http://10.1.2.3:11434", ParameterSize: "11.9B", Digest: "sha256:not-hex-at-all"}},
			&EngineInfo{SizeBytes: 9_300_000_000, VRAMBytes: 9_300_000_000, GPUPercent: 100, ContextLength: 32768,
				ParameterSize: "11.9B"}, false},
		{"another model is loaded", []ollamatest.LoadedSpec{{Name: "other:1b", Size: 1, SizeVRAM: 1}}, nil, true},
		{"the engine reports no size", []ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768}}, nil, true},
		{"nothing is loaded", nil, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tasks := corpusWithTriage(t)
			srv := fakeOllama(t, ollamatest.Turn{Content: oomAnswer})
			srv.Loaded = c.loaded
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
	srv := fakeOllama(t, ollamatest.Turn{Content: oomAnswer})
	srv.PsStatus = 500
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

func TestRunFindsAnUntaggedModelLoadedAsLatest(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:latest", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Loaded = []ollamatest.LoadedSpec{{Name: "gemma4:latest", ContextLength: 32768, Size: 5_000_000_000, SizeVRAM: 5_000_000_000}}
	srv.Turns = []ollamatest.Turn{{Content: "ready"}, {Content: oomAnswer}}
	opts := runOptions(srv, "")
	opts.Model = "gemma4"
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Engine == nil || res.Engine.GPUPercent != 100 {
		t.Fatalf("engine %+v", res.Engine)
	}
}

func TestRunWritesTheHardwareLabel(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	opts := runOptions(fakeOllama(t, ollamatest.Turn{Content: oomAnswer}), "")
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
		{"http://10.0.0.3:11434", "", true},              // a URL is an address, not a label
		{"the box at 10.0.0.3", "", true},                // so is a bare IP address
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

// TestEngineInfoFailsWithoutAHost: the capture reports why it has nothing, it never panics.
func TestEngineInfoFailsWithoutAHost(t *testing.T) {
	info, err := engineInfo(context.Background(), RunOptions{Model: "gemma4:12b"})
	if info != nil || err == nil {
		t.Fatalf("info %+v, err %v", info, err)
	}
}
