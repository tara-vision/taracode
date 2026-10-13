package taraenginetest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tara-vision/taraengine/pkg/api"
)

// call sends one request to the fake and returns the status, the headers and the body.
func call(t *testing.T, srv *Server, method, path, auth, body string) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, data
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return v
}

func TestHealthIsOpenAndNamesTheBackend(t *testing.T) {
	srv := New(t)
	srv.Key = "k"
	status, _, data := call(t, srv, http.MethodGet, "/api/health", "", "")
	if h := decode[api.Health](t, data); status != http.StatusOK || h.Status != "ok" || h.Backend != "llama.cpp" {
		t.Fatalf("health %d %+v", status, h)
	}
	srv.Health = api.Health{Status: "degraded", Backend: "llama.cpp", Detail: "llama-server is not running"}
	if _, _, data = call(t, srv, http.MethodGet, "/api/health", "", ""); decode[api.Health](t, data).Detail == "" {
		t.Fatalf("a scripted health answer: %s", data)
	}
}

func TestEveryOtherRouteWantsTheKey(t *testing.T) {
	srv := New(t)
	srv.Key = "k"
	status, _, data := call(t, srv, http.MethodGet, "/api/version", "", "")
	refused := decode[api.Error](t, data)
	if status != http.StatusUnauthorized || refused.Error.Hint != "send Authorization: Bearer <api_key>" {
		t.Fatalf("without the key: %d %s", status, data)
	}
	status, _, data = call(t, srv, http.MethodGet, "/api/version", "Bearer k", "")
	v := decode[api.Version](t, data)
	if status != http.StatusOK || v.Version != DefaultVersion || v.Runtime.Build != DefaultRuntime ||
		v.Platform != DefaultPlatform {
		t.Fatalf("with the key: %d %+v", status, v)
	}
}

func TestModelsAnswerFromTheSpecs(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 131072, Family: "gemma4",
			ParameterSize: "12B", Quantization: "Q4_K_M", Size: 7_600_000_000, Tier: "16", Default: true,
			Digest: "a1b2c3d4e5f6"},
		{Name: "glm-4.7-flash", Status: "available"},
		{Name: "bad:1b", Status: "faulty", ExitCode: 1, LogTail: "CUDA error: out of memory"},
	}
	_, _, data := call(t, srv, http.MethodGet, "/api/models", "", "")
	list := decode[[]api.Model](t, data)
	if len(list) != 3 || list[0].Status != "pulled" || list[0].NativeContext != 131072 || !list[0].Default ||
		list[1].Status != "available" || list[1].Capabilities == nil || list[2].ExitCode != 1 {
		t.Fatalf("models: %+v", list)
	}
	_, _, data = call(t, srv, http.MethodGet, "/api/models/bad:1b", "", "")
	if d := decode[api.ModelDetail](t, data); d.LogTail != "CUDA error: out of memory" || d.Status != "faulty" {
		t.Fatalf("detail: %+v", d)
	}
	_, _, data = call(t, srv, http.MethodGet, "/api/models/gemma4:12b", "", "")
	if d := decode[api.ModelDetail](t, data); d.Digest != "a1b2c3d4e5f6" || d.Quantization != "Q4_K_M" {
		t.Fatalf("detail: %+v", d)
	}
	status, _, data := call(t, srv, http.MethodGet, "/api/models/nope:1b", "", "")
	if status != http.StatusNotFound || !strings.Contains(string(data), "unknown model nope:1b") {
		t.Fatalf("an unknown model: %d %s", status, data)
	}
}

// TestAnImportReportsItsCapabilitiesAsInferred: an import lists the capabilities the spec gives it, none before the
// engine has loaded it, and marks them inferred; a catalog model does not.
func TestAnImportReportsItsCapabilitiesAsInferred(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{{Name: "owner/repo:q4", CapabilitiesInferred: true},
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}}}
	_, _, data := call(t, srv, http.MethodGet, "/api/models", "", "")
	list := decode[[]api.Model](t, data)
	if len(list) != 2 || !list[0].CapabilitiesInferred || len(list[0].Capabilities) != 0 || list[1].CapabilitiesInferred {
		t.Fatalf("models: %+v", list)
	}
	_, _, data = call(t, srv, http.MethodGet, "/api/models/owner/repo:q4", "", "")
	if d := decode[api.ModelDetail](t, data); !d.CapabilitiesInferred || d.Capabilities == nil {
		t.Fatalf("detail: %+v", d)
	}
}

func TestLoadedAndDoctorAnswerFromTheSpecs(t *testing.T) {
	srv := New(t)
	srv.Loaded = []LoadedSpec{
		{Name: "gemma4:12b", ContextLength: 32768, Size: 10_000_000_000},
		{Name: "vram:1b", ContextLength: 4096, SizeVRAM: 2_000_000_000, Source: "estimated"},
		{Name: "bare:1b", ContextLength: 2048, Source: "none"},
	}
	_, _, data := call(t, srv, http.MethodGet, "/api/loaded", "", "")
	loaded := decode[[]api.Loaded](t, data)
	if loaded[0].Memory.Bytes != 10_000_000_000 || loaded[0].Memory.Source != "measured" ||
		loaded[1].Memory.Bytes != 2_000_000_000 || loaded[1].Memory.Source != "estimated" || loaded[2].Memory != nil {
		t.Fatalf("loaded: %+v", loaded)
	}
	_, _, data = call(t, srv, http.MethodGet, "/api/doctor", "", "")
	if d := decode[api.Doctor](t, data); d.Machine.Tier != "32" || d.Recommendation["32"] != "glm-4.7-flash" {
		t.Fatalf("doctor: %+v", d)
	}
}

func TestModelRoutesTakeNamesWithASlash(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{{Name: "owner/repo:q4", Capabilities: []string{"tools"}}}
	status, _, data := call(t, srv, http.MethodGet, "/api/models/owner/repo:q4", "", "")
	if d := decode[api.ModelDetail](t, data); status != http.StatusOK || d.Name != "owner/repo:q4" {
		t.Fatalf("a name with a slash: %d %s", status, data)
	}
}

func TestLoadRecordsAndRefusesAsTheEngineDoes(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{{Name: "gemma4:12b", ContextLength: 131072}, {Name: "bad:1b", Status: "faulty"},
		{Name: "glm-4.7-flash", Status: "available"}}
	status, _, data := call(t, srv, http.MethodPost, "/api/load", "", `{"model":"gemma4:12b","context":32768}`)
	if m := decode[api.Model](t, data); status != http.StatusOK || m.Context != 32768 || m.Status != "loaded" {
		t.Fatalf("load: %d %s", status, data)
	}
	if status, _, _ = call(t, srv, http.MethodPost, "/api/load", "", `{"model":"bad:1b"}`); status != http.StatusConflict {
		t.Fatalf("a faulty model without force: %d", status)
	}
	if status, _, _ = call(t, srv, http.MethodPost, "/api/load", "", `{"model":"bad:1b","force":true}`); status != http.StatusOK {
		t.Fatalf("a faulty model with force: %d", status)
	}
	for _, name := range []string{"glm-4.7-flash", "nope:1b"} {
		status, _, data = call(t, srv, http.MethodPost, "/api/load", "", `{"model":"`+name+`"}`)
		if status != http.StatusNotFound || decode[api.Error](t, data).Error.Hint == "" {
			t.Fatalf("%s: %d %s", name, status, data)
		}
	}
	want := []LoadRequest{{"gemma4:12b", 32768, false}, {"bad:1b", 0, false}, {"bad:1b", 0, true},
		{"glm-4.7-flash", 0, false}, {"nope:1b", 0, false}}
	if len(srv.Loads) != len(want) || srv.Loads[0] != want[0] || srv.Loads[2] != want[2] {
		t.Fatalf("loads: %+v", srv.Loads)
	}
	if status, _, _ = call(t, srv, http.MethodPost, "/api/unload", "", `{"model":"gemma4:12b"}`); status != http.StatusNoContent ||
		len(srv.Unloaded) != 1 || srv.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("unload: %d %v", status, srv.Unloaded)
	}
}

func TestFailuresAnswerForTheirCallsThenStop(t *testing.T) {
	srv := New(t)
	srv.Failures["GET /api/version"] = &Failure{Status: http.StatusServiceUnavailable,
		Message: "the engine backend is not running", Hint: "wait a moment and retry", Times: 1}
	srv.Failures["GET /api/doctor"] = &Failure{Status: http.StatusInternalServerError, Message: "broken"}
	if status, _, _ := call(t, srv, http.MethodGet, "/api/version", "", ""); status != http.StatusServiceUnavailable {
		t.Fatalf("the first call fails: %d", status)
	}
	if status, _, _ := call(t, srv, http.MethodGet, "/api/version", "", ""); status != http.StatusOK {
		t.Fatalf("the second call answers: %d", status)
	}
	for range 2 {
		if status, _, _ := call(t, srv, http.MethodGet, "/api/doctor", "", ""); status != http.StatusInternalServerError {
			t.Fatalf("a failure without Times fails every call: %d", status)
		}
	}
}

func TestRequestsAreRecorded(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{{Name: "gemma4:12b"}}
	call(t, srv, http.MethodPost, "/api/load", "Bearer k", `{"model":"gemma4:12b"}`)
	call(t, srv, http.MethodGet, "/api/health", "", "")
	got := srv.Requests
	if len(got) != 2 || got[0].Method != http.MethodPost || got[0].Path != "/api/load" || got[0].Auth != "Bearer k" ||
		got[0].Body["model"] != "gemma4:12b" || got[1].Path != "/api/health" || got[1].Body != nil {
		t.Fatalf("requests: %+v", got)
	}
}

func TestUnreadableBodiesAreRecordedEmpty(t *testing.T) {
	srv := New(t)
	call(t, srv, http.MethodPost, "/api/unload", "", "not json")
	if srv.Requests[0].Body != nil || len(srv.Unloaded) != 1 || srv.Unloaded[0] != "" {
		t.Fatalf("a body that is not JSON: %+v %q", srv.Requests[0], srv.Unloaded)
	}
}
