package models

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tara-vision/taraengine/pkg/api"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// noLookup is the lookPath stub every test that does not care about external tools passes.
func noLookup(string) (string, error) { return "", errors.New("not found") }

// window32k settles on a 32,768-token window with no note.
func window32k(int) (int, string) { return 32768, "" }

// openAIStub is an OpenAI-compatible server as llm.Client sees it: a version and a model list, nothing else.
type openAIStub struct {
	version    string
	versionErr error
	models     []llm.ModelInfo
	modelsErr  error
}

func (s openAIStub) Chat(context.Context, llm.Request, func(llm.Event) error) (*llm.Result, error) {
	return nil, llm.ErrNotSupported
}
func (s openAIStub) Models(context.Context) ([]llm.ModelInfo, error) { return s.models, s.modelsErr }
func (s openAIStub) Show(context.Context, string) (*llm.ModelDetails, error) {
	return nil, llm.ErrNotSupported
}
func (s openAIStub) Loaded(context.Context) ([]llm.LoadedModel, error) {
	return nil, llm.ErrNotSupported
}
func (s openAIStub) Version(context.Context) (string, error) { return s.version, s.versionErr }
func (s openAIStub) Unload(context.Context, string) error    { return llm.ErrNotSupported }

func engineWithModels(t *testing.T) *taraenginetest.Server {
	t.Helper()
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{
		{Name: "qwen3.6:35b", Capabilities: []string{"completion", "tools"}, ContextLength: 262144,
			Size: 22_000_000_000, Status: "loaded"},
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools", "thinking", "vision"}, ContextLength: 131072,
			Size: 7_600_000_000},
		{Name: "glm-4.7-flash", Status: "available"},
	}
	return srv
}

func diagnose(srv *taraenginetest.Server, key, model string, keepAlive bool) Report {
	return Diagnose(context.Background(), taraengine.New(srv.URL, key, nil), srv.URL, model, keepAlive, noLookup,
		window32k)
}

func TestDiagnoseReadsTheEngine(t *testing.T) {
	srv := engineWithModels(t)
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 16384, Size: 9_000_000_000}}
	rep := diagnose(srv, "", "gemma4:12b", true)
	if !rep.ServerOK || rep.Engine == nil || rep.Engine.Version != "0.2.0" || rep.Engine.Runtime != "b11514" ||
		rep.Engine.Recommended != "glm-4.7-flash" || rep.Engine.Machine.Tier != "32" {
		t.Fatalf("engine section: %+v %+v", rep, rep.Engine)
	}
	if len(rep.Models) != 2 || rep.Models[0].Name != "gemma4:12b" || !rep.Models[0].Vision ||
		rep.Models[1].Status != "loaded" {
		t.Fatalf("models: %+v", rep.Models)
	}
	if rep.LoadedContext != 16384 || rep.ContextWindow != 32768 {
		t.Fatalf("context: %+v", rep)
	}
	out := rep.Render()
	for _, want := range []string{
		"taraengine 0.2.0 (runtime b11514, linux/amd64/cuda12)",
		"Machine   linux/amd64, NVIDIA GeForce RTX 5090 (32 GiB), 48 GiB RAM, tier 32",
		"tools thinking vision", "loaded",
		"Model     gemma4:12b (loaded at a 16384-token window)",
		"Context   32768 tokens, set when the model loads; the next session reloads it",
		"Keep      keep_alive in the config does nothing: taracode sends it to no server\n",
		"Advice    recommended for tier 32: glm-4.7-flash\n          taraengine pull glm-4.7-flash",
		"helm: not found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

// TestDiagnoseShowsTheWindowNote: a warning from resolving the window prints right under the Context line.
func TestDiagnoseShowsTheWindowNote(t *testing.T) {
	srv := engineWithModels(t)
	capped := func(int) (int, string) { return 8192, "capped by context.window" }
	out := Diagnose(context.Background(), taraengine.New(srv.URL, "", nil), srv.URL, "gemma4:12b", false, noLookup,
		capped).Render()
	if !strings.Contains(out, "Context   8192 tokens, set when the model loads\n          capped by context.window\n") {
		t.Fatalf("render lacks the window note under the context line:\n%s", out)
	}
}

func TestDiagnoseNotesTheEnginesStateAndWarnings(t *testing.T) {
	srv := engineWithModels(t)
	srv.Version = "0.1.0"
	srv.Health = api.Health{Status: "degraded", Backend: "llama.cpp", Detail: "llama-server is not running"}
	srv.Doctor.Runtime = api.RuntimeStatus{Installed: true}
	srv.Doctor.Recommendation["32"] = "gemma4:12b"
	srv.Doctor.Machine.GPU = ""
	out := diagnose(srv, "", "gemma4:12b", false).Render()
	for _, want := range []string{
		"degraded: llama-server is not running",
		"Machine   linux/amd64, no GPU, 48 GiB RAM, tier 32",
		"taracode supports taraengine 0.2.0 or newer; update the engine",
		"Runtime   installed but not verified: taraengine runtime install",
		"Advice    gemma4:12b is pulled and is the recommended model for this machine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Keep ") {
		t.Errorf("no keep_alive note without keep_alive:\n%s", out)
	}
}

// TestDiagnoseSaysTheConfiguredModelSleeps: the engine lists a sleeping model among the loaded ones, with its window,
// so the Model line says it sleeps at that window; a model that runs is loaded at it.
func TestDiagnoseSaysTheConfiguredModelSleeps(t *testing.T) {
	srv := engineWithModels(t)
	srv.Models[1].Status = "sleeping"
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_000_000_000}}
	out := diagnose(srv, "", "gemma4:12b", false).Render()
	if !strings.Contains(out, "Model     gemma4:12b (sleeping at a 32768-token window)\n") ||
		strings.Contains(out, "loaded at a") {
		t.Fatalf("a sleeping model:\n%s", out)
	}
	srv.Models[1].Status = "loaded"
	if out := diagnose(srv, "", "gemma4:12b", false).Render(); !strings.Contains(out,
		"Model     gemma4:12b (loaded at a 32768-token window)\n") {
		t.Fatalf("a loaded model:\n%s", out)
	}
}

func TestDiagnoseShowsAFaultyModel(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{{Name: "bad:1b", Capabilities: []string{"tools"}, Status: "faulty",
		ExitCode: 1, LogTail: "line one\nline two\n"}}
	out := diagnose(srv, "", "bad:1b", false).Render()
	for _, want := range []string{"faulty, exit code 1: taraengine load bad:1b --force tries it again",
		"          | line one\n          | line two\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

// TestDiagnoseKeepsTheEnginesTextOffTheTerminalControls: the engine's own text (the health detail, the machine, the
// runtime, the platform, the recommendation, and a faulty model's log tail, which echoes an imported model's metadata)
// reaches the terminal with its control characters dropped and its newlines kept.
func TestDiagnoseKeepsTheEnginesTextOffTheTerminalControls(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{{Name: "bad:1b", Capabilities: []string{"tools"}, Status: "faulty",
		ExitCode: 1, LogTail: "general.name \x1b]0;title\x07x\nline two\x1b[2J\n"}}
	srv.Runtime, srv.Platform = "b1\x1b[31m", "linux\x1b[0m/amd64"
	srv.Health = api.Health{Status: "degraded", Backend: "llama.cpp", Detail: "gone\x1b[2J"}
	srv.Doctor.Machine.GPU = "RTX\x1b[1m 5090"
	srv.Doctor.Runtime = api.RuntimeStatus{Installed: true, Error: "bad\x1b[5m checksum"}
	srv.Doctor.Recommendation["32"] = "glm\x1b[7m-4.7-flash"
	out := diagnose(srv, "", "bad:1b", false).Render()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("control characters reach the terminal:\n%q", out)
	}
	for _, want := range []string{"(runtime b1[31m, linux[0m/amd64)", "degraded: gone[2J", "RTX[1m 5090",
		"Runtime   bad[5m checksum", "glm[7m-4.7-flash", "| general.name ]0;titlex\n          | line two[2J\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

func TestDiagnoseNamesAModelTaracodeCannotUse(t *testing.T) {
	srv := engineWithModels(t)
	srv.Models = append(srv.Models, taraenginetest.ModelSpec{Name: "embed:1b", Capabilities: []string{"embedding"}})
	cases := map[string]string{
		"glm-4.7-flash": "Model     glm-4.7-flash (not pulled: taraengine pull glm-4.7-flash)",
		"embed:1b":      "Model     embed:1b (no tool calls: taracode needs a model that has them)",
	}
	for model, want := range cases {
		if out := diagnose(srv, "", model, false).Render(); !strings.Contains(out, want) {
			t.Errorf("%s: render lacks %q:\n%s", model, want, out)
		}
	}
}

// TestDiagnoseSaysAnImportsCapabilitiesShowOnceItLoads: the engine lists none for an import it has not loaded and marks
// them inferred. The session starts such a model, so doctor does not say it has no tool calls: the Model line says the
// capabilities show once it loads, and the capabilities column says they are unknown until then. The server is still
// ok, which is all the exit status reads.
func TestDiagnoseSaysAnImportsCapabilitiesShowOnceItLoads(t *testing.T) {
	srv := engineWithModels(t)
	srv.Models = append(srv.Models,
		taraenginetest.ModelSpec{Name: "owner/repo:q4", CapabilitiesInferred: true, ContextLength: 16384,
			Size: 2_000_000_000},
		taraenginetest.ModelSpec{Name: "owner/other:q8", CapabilitiesInferred: true, ContextLength: 8192})
	rep := diagnose(srv, "", "owner/repo:q4", false)
	out := rep.Render()
	if !rep.ServerOK {
		t.Fatalf("the server is ok: %+v", rep)
	}
	// name to 28, a space, the size and window, the capabilities to 22, a space, the state
	wantRow := "          owner/repo:q4" + strings.Repeat(" ", 16) + "  2.0 GB  ctx 16384   unknown until loaded   pulled\n"
	for _, want := range []string{"Model     owner/repo:q4 (capabilities show once it loads)\n", wantRow} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "no tool calls") {
		t.Errorf("an import is not a model without tools:\n%s", out)
	}
	// a model that is not the configured one gets the column and no note
	if !strings.Contains(out, "owner/other:q8") || strings.Count(out, "unknown until loaded") != 2 ||
		strings.Count(out, "capabilities show once it loads") != 1 {
		t.Errorf("each import reads unknown, the configured one has the note:\n%s", out)
	}
}

// TestDiagnoseShowsTheCapabilitiesOfAnImportTheEngineHasLoaded: once the import has loaded, the engine still marks the
// capabilities inferred but lists them, and doctor shows them as it does for any model, with no note.
func TestDiagnoseShowsTheCapabilitiesOfAnImportTheEngineHasLoaded(t *testing.T) {
	srv := engineWithModels(t)
	srv.Models = append(srv.Models, taraenginetest.ModelSpec{Name: "owner/repo:q4", CapabilitiesInferred: true,
		Capabilities: []string{"completion", "tools", "thinking"}, ContextLength: 16384, Size: 2_000_000_000})
	out := diagnose(srv, "", "owner/repo:q4", false).Render()
	for _, want := range []string{"Model     owner/repo:q4\n",
		"ctx 16384   tools thinking" + strings.Repeat(" ", 9) + "pulled\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"unknown until loaded", "capabilities show once it loads", "no tool calls"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("render has %q for an import whose capabilities are listed:\n%s", unwanted, out)
		}
	}
}

func TestDiagnoseReportsAWrongKey(t *testing.T) {
	srv := engineWithModels(t)
	srv.Key = "right"
	rep := diagnose(srv, "wrong", "gemma4:12b", false)
	out := rep.Render()
	if rep.ServerOK || !rep.KeyNeeded || !strings.Contains(out, "          refused: missing or wrong API key") ||
		strings.Contains(out, "unreachable") || !strings.Contains(out, "set key in ~/.taracode/config.yaml, or pass --key") {
		t.Fatalf("a wrong key: %+v\n%s", rep, out)
	}
}

func TestDiagnoseWhenAnEngineCallFails(t *testing.T) {
	srv := engineWithModels(t)
	srv.Failures["GET /api/models"] = &taraenginetest.Failure{Status: http.StatusInternalServerError, Message: "broken"}
	if rep := diagnose(srv, "", "gemma4:12b", false); rep.ServerOK || rep.Engine != nil ||
		!strings.Contains(rep.Render(), "          refused: broken\n") {
		t.Fatalf("models fail: %+v\n%s", rep, rep.Render())
	}
	srv = engineWithModels(t)
	srv.Models[1].Status = "faulty"
	for _, route := range []string{"GET /api/doctor", "GET /api/loaded", "GET /api/models/gemma4:12b"} {
		srv.Failures[route] = &taraenginetest.Failure{Status: http.StatusInternalServerError, Message: "broken"}
	}
	rep := diagnose(srv, "", "gemma4:12b", false)
	if !rep.ServerOK || rep.Engine.Machine.OS != "" || rep.LoadedContext != 0 || rep.Engine.Faulty != nil {
		t.Fatalf("doctor, loaded and the detail fail: %+v %+v", rep, rep.Engine)
	}
	if strings.Contains(rep.Render(), "Machine ") {
		t.Errorf("no machine line without the engine's doctor:\n%s", rep.Render())
	}
	gone := Diagnose(context.Background(), taraengine.New("http://127.0.0.1:1", "", nil), "http://127.0.0.1:1", "", false,
		noLookup, nil)
	if gone.ServerOK || !gone.InstallHint || !strings.Contains(gone.Render(), "          unreachable: nothing answers at") {
		t.Fatalf("an engine nothing answers at: %+v\n%s", gone, gone.Render())
	}
}

func TestRuntimeNote(t *testing.T) {
	cases := map[string]api.RuntimeStatus{
		"":                  {Installed: true, Verified: true},
		"checksum mismatch": {Installed: true, Error: "checksum mismatch"},
		"not installed: taraengine runtime install":              {},
		"installed but not verified: taraengine runtime install": {Installed: true},
	}
	for want, status := range cases {
		if got := runtimeNote(status); got != want {
			t.Errorf("runtimeNote(%+v) = %q, want %q", status, got, want)
		}
	}
}

// TestDoctorPrintsAHostWithoutItsPassword: the host comes from the configuration, where a user:password may stand in
// its URL, and doctor's output is the kind of text people paste into a report. The Server line masks the password as
// the unreachable line does, and a host without user info prints as it is.
func TestDoctorPrintsAHostWithoutItsPassword(t *testing.T) {
	srv := engineWithModels(t)
	withPassword := strings.Replace(srv.URL, "http://", "http://user:secret@", 1)
	masked := "Server    " + strings.Replace(srv.URL, "http://", "http://user:xxxxx@", 1) + "\n"
	reports := map[string]Report{
		"an engine": Diagnose(context.Background(), taraengine.New(srv.URL, "", nil), withPassword, "gemma4:12b", false,
			noLookup, window32k),
		"an OpenAI-compatible server": Diagnose(context.Background(), openAIStub{version: "vllm 0.11"}, withPassword, "",
			false, noLookup, nil),
		"nothing answering": UnreachableReport(withPassword, &llm.UnreachableError{Host: withPassword}, "", noLookup),
	}
	for name, rep := range reports {
		if out := rep.Render(); !strings.Contains(out, masked) || strings.Contains(out, "secret") {
			t.Errorf("%s: want %q and no password:\n%s", name, masked, out)
		}
	}
	if out := diagnose(srv, "", "gemma4:12b", false).Render(); !strings.Contains(out, "Server    "+srv.URL+"\n") {
		t.Errorf("a host without user info prints as it is:\n%s", out)
	}
}

func TestUnreachableReportPrintsTheInstallLine(t *testing.T) {
	hinted := &llm.UnreachableError{Host: "http://127.0.0.1:8272", Hint: taraengine.StartHint}
	out := UnreachableReport("http://127.0.0.1:8272", hinted, "gemma4:12b", noLookup).Render()
	for _, want := range []string{
		"unreachable: nothing answers at http://127.0.0.1:8272: " + taraengine.StartHint,
		"install it: " + taraengine.InstallHint, "Model     gemma4:12b\n", "kubectl: not found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	bare := UnreachableReport("http://gpu:1", &llm.UnreachableError{Host: "http://gpu:1"}, "", noLookup)
	if strings.Contains(bare.Render(), "install it:") {
		t.Errorf("a host the user chose gets no install line:\n%s", bare.Render())
	}
}

func TestDiagnoseAnOpenAICompatibleServer(t *testing.T) {
	stub := openAIStub{version: "vllm 0.11", models: []llm.ModelInfo{{Name: "b", Size: 2e9}, {Name: "a"}}}
	gitOnly := func(name string) (string, error) {
		if name == "git" {
			return "/usr/bin/git", nil
		}
		return "", errors.New("not found")
	}
	rep := Diagnose(context.Background(), stub, "http://vllm:8000", "a", false, gitOnly, window32k)
	out := rep.Render()
	if !rep.ServerOK || rep.Models[0].Name != "a" || rep.ContextWindow != 0 || !strings.Contains(out, "git: /usr/bin/git") ||
		!strings.Contains(out, "          vllm 0.11 reachable\n") || strings.Contains(out, "Context ") {
		t.Fatalf("an OpenAI-compatible server: %+v\n%s", rep, out)
	}
	// The OpenAI model list has names only: a model whose size and window are both unknown is its name alone.
	if !strings.Contains(out, "Models\n          a\n          b ") || !strings.Contains(out, "2.0 GB") ||
		strings.Contains(out, "0.0 GB") {
		t.Errorf("the model rows:\n%s", out)
	}
	if strings.Contains(out, "Keep ") {
		t.Errorf("no keep_alive note without keep_alive:\n%s", out)
	}
	withKeepAlive := Diagnose(context.Background(), stub, "http://vllm:8000", "a", true, gitOnly, window32k).Render()
	if !strings.Contains(withKeepAlive, "Keep      keep_alive in the config does nothing: taracode sends it to no server\n") {
		t.Errorf("the keep_alive note holds on any server, not only the engine:\n%s", withKeepAlive)
	}
	stub.version = ""
	if out := Diagnose(context.Background(), stub, "h", "", false, noLookup, nil).Render(); !strings.Contains(out,
		"          server reachable\n") {
		t.Errorf("no version:\n%s", out)
	}
	for _, failing := range []openAIStub{{versionErr: errors.New("refused")}, {modelsErr: errors.New("refused")}} {
		if rep := Diagnose(context.Background(), failing, "h", "", false, noLookup, nil); rep.ServerOK ||
			rep.ServerError != "refused" {
			t.Errorf("a failing server: %+v", rep)
		}
	}
}

func TestRenderShowsThePolicyNote(t *testing.T) {
	rep := Report{Host: "h", ServerOK: true, Tools: map[string]string{}, PolicyNote: "ok (built-in)"}
	if !strings.Contains(rep.Render(), "Policy    ok (built-in)") {
		t.Fatalf("render lacks the policy line:\n%s", rep.Render())
	}
}

// TestDiagnoseReportsAStartingEngineWithoutWaiting: an engine that is still getting ready is reported as it is, with
// the step it is on; the models are not listed, since the engine answers nothing about them yet, and doctor asks its
// health once instead of waiting.
func TestDiagnoseReportsAStartingEngineWithoutWaiting(t *testing.T) {
	srv := engineWithModels(t)
	srv.Starting = []string{"installing runtime b11514 (65%)\x1b[2J", "installing runtime b11514 (70%)"}
	rep := diagnose(srv, "", "gemma4:12b", false)
	out := rep.Render()
	for _, want := range []string{
		"          taraengine 0.2.0 (runtime b11514, linux/amd64/cuda12)\n",
		"          starting: installing runtime b11514 (65%)[2J\n",
		"Models    listed once the engine is ready\n",
		"Model     gemma4:12b\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if !rep.ServerOK || rep.Engine.Degraded != "" || strings.Contains(out, "degraded") || len(rep.Models) != 0 {
		t.Errorf("report %+v:\n%s", rep, out)
	}
	health := 0
	for _, r := range srv.Requests {
		if r.Path == "/api/health" {
			health++
		}
	}
	if health != 1 {
		t.Errorf("health asked %d times, want once", health)
	}
}
