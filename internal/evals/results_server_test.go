package evals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func TestServerOfReadsBothForms(t *testing.T) {
	engine := Results{Server: &ServerInfo{Name: "taraengine", Version: "0.2.0", Runtime: "b11514"}}
	legacy := Results{Ollama: "0.35.0"}
	if got := engine.ServerOf(); got != *engine.Server || got.String() != "taraengine 0.2.0" {
		t.Errorf("engine: %+v %q", got, got.String())
	}
	if got := legacy.ServerOf(); got != (ServerInfo{Name: "ollama", Version: "0.35.0"}) {
		t.Errorf("legacy: %+v", got)
	}
	if got := (Results{}).ServerOf(); got != (ServerInfo{}) || got.String() != "" {
		t.Errorf("none: %+v", got)
	}
}

func TestResultsNameKeepsSameDayRunsApart(t *testing.T) {
	engine := Results{Model: "gemma4:12b", Date: "2026-10-25", Server: &ServerInfo{Name: "taraengine"}}
	legacy := Results{Model: "gemma4:12b", Date: "2026-10-25", Ollama: "0.35.0"}
	if ResultsName(engine) != "gemma4-12b-taraengine-2026-10-25" || ResultsName(legacy) != "gemma4-12b-2026-10-25" {
		t.Fatalf("%q %q", ResultsName(engine), ResultsName(legacy))
	}
	dir := t.TempDir()
	for _, r := range []Results{engine, legacy} {
		if _, err := WriteResults(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("same-day runs on two engines: %d files", len(entries))
	}
}

func TestNewResultsKeepTheOldKeysOrder(t *testing.T) {
	data, err := json.Marshal(Results{Taracode: "3.3.0", Server: &ServerInfo{Name: "taraengine", Version: "0.2.0"},
		Model: "gemma4:12b", Tier: "16", Default: true})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, `"ollama"`) || !strings.Contains(got, `"server":{"name":"taraengine","version":"0.2.0"}`) ||
		!strings.Contains(got, `"tier":"16","default":true`) {
		t.Fatalf("a new result: %s", got)
	}
}

func TestRunRecordsTheServerTierAndDefault(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, engineTurn("OOMKilled memory limit"))
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := ServerInfo{Name: "taraengine", Version: "0.2.0", Runtime: "b11514"}
	if res.Server == nil || *res.Server != want || res.Ollama != "" || res.Tier != "16" || !res.Default {
		t.Fatalf("header %+v %+v", res, res.Server)
	}
}

func TestRunNamesTheOtherTierForAModelOutsideTheCatalog(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, engineTurn("OOMKilled memory limit"))
	srv.Models[0].Tier, srv.Models[0].Default = "", false
	if res, err := Run(context.Background(), tasks, runOptions(srv, "")); err != nil || res.Tier != "other" ||
		res.Default {
		t.Fatalf("an import: %+v %v", res, err)
	}
	srv = fakeEngine(t, engineTurn("OOMKilled memory limit"))
	srv.Failures["GET /api/models"] = failure500()
	if _, err := checkEngine(context.Background(), withDefaults(runOptions(srv, ""))); err == nil ||
		!strings.Contains(err.Error(), "the engine's catalog") {
		t.Fatalf("a catalog that cannot be read: %v", err)
	}
}

// TestCheckEngineTakesAnImportItCannotSeeTheToolsOf: an import the engine has not loaded lists no capabilities, so the
// check cannot refuse it for missing tools; a catalog model without tools is still refused.
func TestCheckEngineTakesAnImportItCannotSeeTheToolsOf(t *testing.T) {
	srv := fakeEngine(t)
	srv.Models = append(srv.Models, taraenginetest.ModelSpec{Name: "owner/repo:q4", CapabilitiesInferred: true},
		taraenginetest.ModelSpec{Name: "embed:1b", Capabilities: []string{"embedding"}, Tier: "16"})
	opts := withDefaults(runOptions(srv, ""))
	opts.Model = "owner/repo:q4"
	if res, err := checkEngine(context.Background(), opts); err != nil || res.Model != "owner/repo:q4" ||
		res.Tier != "other" {
		t.Fatalf("an import: %+v %v", res, err)
	}
	opts.Model = "embed:1b"
	if _, err := checkEngine(context.Background(), opts); err == nil ||
		!strings.Contains(err.Error(), "has no tools capability") {
		t.Fatalf("a catalog model without tools: %v", err)
	}
}

func TestRunRefusesAnEngineThatCannotSayItsVersion(t *testing.T) {
	srv := fakeEngine(t)
	srv.Failures["GET /api/version"] = failure500()
	if _, err := checkEngine(context.Background(), withDefaults(runOptions(srv, ""))); err == nil ||
		!strings.Contains(err.Error(), "the engine's version") {
		t.Fatalf("a version that cannot be read: %v", err)
	}
}

func TestRunRefusesAServerThatIsNotTheEngine(t *testing.T) {
	vllm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"gemma4:12b"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(vllm.Close)
	opts := withDefaults(RunOptions{Host: vllm.URL, Model: "gemma4:12b"})
	if _, err := checkEngine(context.Background(), opts); err == nil || !strings.Contains(err.Error(),
		"evals run on taraengine") {
		t.Fatalf("a vLLM server: %v", err)
	}
}

// TestRunNamesTheHostOfAServerThatIsNotTheEngineWithoutItsPassword: the refusal names the host the run was asked for,
// where a user:password may stand in the URL, and the error reaches the terminal. The password is masked. The vendor is
// given, so nothing is dialled.
func TestRunNamesTheHostOfAServerThatIsNotTheEngineWithoutItsPassword(t *testing.T) {
	opts := withDefaults(RunOptions{Host: "http://user:secret@example.com:8272", Vendor: "vllm", Model: "gemma4:12b"})
	_, err := checkEngine(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "evals run on taraengine; http://user:xxxxx@example.com:8272 is ") ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("a vLLM server behind a host with a password: %v", err)
	}
}

func engineTurn(text string) taraenginetest.Turn { return taraenginetest.Turn{Content: text} }

func failure500() *taraenginetest.Failure {
	return &taraenginetest.Failure{Status: http.StatusInternalServerError, Message: "broken"}
}
