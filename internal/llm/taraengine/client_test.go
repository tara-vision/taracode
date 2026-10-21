package taraengine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tara-vision/taraengine/pkg/api"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func engineWith(t *testing.T, models ...taraenginetest.ModelSpec) (*taraenginetest.Server, *Client) {
	t.Helper()
	srv := taraenginetest.New(t)
	srv.Models = models
	return srv, New(srv.URL, "", http.DefaultClient)
}

func TestClientListsThePulledModels(t *testing.T) {
	_, c := engineWith(t,
		taraenginetest.ModelSpec{Name: "gemma4:12b", Family: "gemma4", ParameterSize: "12B", Quantization: "Q4_K_M",
			Size: 7_600_000_000},
		taraenginetest.ModelSpec{Name: "glm-4.7-flash", Status: "available"})
	infos, err := c.Models(context.Background())
	want := llm.ModelInfo{Name: "gemma4:12b", Size: 7_600_000_000, Family: "gemma4", ParameterSize: "12B",
		Quantization: "Q4_K_M"}
	if err != nil || len(infos) != 1 || infos[0] != want {
		t.Fatalf("models %+v %v", infos, err)
	}
}

func TestClientShowMapsTheDetail(t *testing.T) {
	_, c := engineWith(t, taraenginetest.ModelSpec{Name: "gemma4:12b", ContextLength: 131072, Family: "gemma4",
		ParameterSize: "12B", Quantization: "Q4_K_M", Digest: "a1b2c3d4e5f6",
		Capabilities: []string{"completion", "tools", "thinking"}})
	d, err := c.Show(context.Background(), "gemma4:12b")
	if err != nil || !d.Has("tools") || !d.Has("thinking") || d.ContextLength != 131072 || d.Family != "gemma4" ||
		d.ParameterSize != "12B" || d.Quantization != "Q4_K_M" || d.Digest != "a1b2c3d4e5f6" {
		t.Fatalf("show %+v %v", d, err)
	}
}

func TestClientShowRefusesAModelThatIsNotPulled(t *testing.T) {
	_, c := engineWith(t, taraenginetest.ModelSpec{Name: "glm-4.7-flash", Status: "available"})
	_, err := c.Show(context.Background(), "glm-4.7-flash")
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusNotFound || refused.Model != "glm-4.7-flash" ||
		refused.Message != "glm-4.7-flash is not pulled" {
		t.Fatalf("a catalog model that is not pulled: %#v", err)
	}
	if _, err := c.Show(context.Background(), "nope:1b"); !errors.As(err, &refused) {
		t.Fatalf("an unknown model: %#v", err)
	}
}

func TestClientShowsAModelNamedWithASlash(t *testing.T) {
	_, c := engineWith(t, taraenginetest.ModelSpec{Name: "owner/repo:q4", Capabilities: []string{"tools"}})
	if d, err := c.Show(context.Background(), "owner/repo:q4"); err != nil || !d.Has("tools") {
		t.Fatalf("a name with a slash: %+v %v", d, err)
	}
}

// TestClientShowLeavesAnImportsCapabilitiesUnknownUntilItLoads: the engine lists an import's capabilities only while
// it is loaded; before that Show says they are unknown, and once listed they count as a catalog model's do.
func TestClientShowLeavesAnImportsCapabilitiesUnknownUntilItLoads(t *testing.T) {
	_, c := engineWith(t,
		taraenginetest.ModelSpec{Name: "owner/repo:q4", CapabilitiesInferred: true},
		taraenginetest.ModelSpec{Name: "owner/loaded:q4", CapabilitiesInferred: true, Capabilities: []string{"tools"}},
		taraenginetest.ModelSpec{Name: "embed:1b"})
	ctx := context.Background()
	if d, err := c.Show(ctx, "owner/repo:q4"); err != nil || !d.CapabilitiesUnknown || d.Has("tools") {
		t.Fatalf("an import the engine has not loaded: %+v %v", d, err)
	}
	if d, err := c.Show(ctx, "owner/loaded:q4"); err != nil || d.CapabilitiesUnknown || !d.Has("tools") {
		t.Fatalf("a loaded import: %+v %v", d, err)
	}
	if d, err := c.Show(ctx, "embed:1b"); err != nil || d.CapabilitiesUnknown {
		t.Fatalf("a catalog model that lists no capabilities: %+v %v", d, err)
	}
}

func TestClientLoadedCarriesTheMemoryFigure(t *testing.T) {
	srv, c := engineWith(t)
	srv.Loaded = []taraenginetest.LoadedSpec{
		{Name: "gemma4:12b", ContextLength: 32768, Size: 9_000_000_000},
		{Name: "mac:1b", ContextLength: 4096, Size: 2_000_000_000, Source: "estimated"},
		{Name: "bare:1b", ContextLength: 2048, Source: "none"},
	}
	got, err := c.Loaded(context.Background())
	want := []llm.LoadedModel{
		{Name: "gemma4:12b", ContextLength: 32768, Size: 9_000_000_000, SizeVRAM: 9_000_000_000, Measured: true},
		{Name: "mac:1b", ContextLength: 4096, Size: 2_000_000_000, SizeVRAM: 2_000_000_000},
		{Name: "bare:1b", ContextLength: 2048},
	}
	if err != nil || len(got) != len(want) {
		t.Fatalf("loaded %+v %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestClientVersionLoadAndUnload(t *testing.T) {
	srv, c := engineWith(t, taraenginetest.ModelSpec{Name: "gemma4:12b"})
	if v, err := c.Version(context.Background()); err != nil || v != taraenginetest.DefaultVersion {
		t.Fatalf("version %q %v", v, err)
	}
	if err := c.Load(context.Background(), "gemma4:12b", 32768); err != nil || srv.Loads[0].Context != 32768 {
		t.Fatalf("load %v %+v", err, srv.Loads)
	}
	if err := c.Unload(context.Background(), "gemma4:12b"); err != nil || srv.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("unload %v %v", err, srv.Unloaded)
	}
}

func TestClientPassesEngineErrorsOn(t *testing.T) {
	srv, c := engineWith(t)
	for _, route := range []string{"GET /api/models", "GET /api/loaded"} {
		srv.Failures[route] = &taraenginetest.Failure{Status: http.StatusInternalServerError, Message: "catalog unreadable"}
	}
	ctx := context.Background()
	if _, err := c.Models(ctx); err == nil {
		t.Error("models: no error")
	}
	if _, err := c.Loaded(ctx); err == nil {
		t.Error("loaded: no error")
	}
}

func TestClientShowCarriesOriginAndSource(t *testing.T) {
	sha := strings.Repeat("a", 64)
	_, c := engineWith(t, taraenginetest.ModelSpec{Name: "qwen-import", Origin: "import",
		Source: &api.Source{Repo: "unsloth/Qwen3-GGUF", File: "q4.gguf", SHA256: sha}})
	d, err := c.Show(context.Background(), "qwen-import")
	if err != nil || d.Origin != "import" || d.Source == nil || d.Source.Repo != "unsloth/Qwen3-GGUF" ||
		d.Source.File != "q4.gguf" || d.Source.SHA256 != sha {
		t.Fatalf("show %+v %v", d, err)
	}
}
