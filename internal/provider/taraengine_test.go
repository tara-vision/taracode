package provider

import (
	"context"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraengine"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func TestTaraengineProviderManagesTheEnginesModels(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{
		{Name: "gemma4:12b", Family: "gemma4", ParameterSize: "12B", Size: 7_600_000_000},
		{Name: "glm-4.7-flash", Status: "available"},
	}
	p := NewTaraengineProvider(srv.URL, "")
	if p.Info().Type != TypeTaraengine || p.Info().Name != "taraengine" || !p.Info().SupportsTools {
		t.Fatalf("info %+v", p.Info())
	}
	if _, ok := p.LLM().(*taraengine.Client); !ok {
		t.Fatalf("LLM() is %T, want the engine client", p.LLM())
	}
	names, err := p.DetectModels(context.Background())
	if err != nil || len(names) != 1 || names[0] != "gemma4:12b" || p.Info().Models[0] != "gemma4:12b" {
		t.Fatalf("DetectModels %v %v", names, err)
	}
	infos, err := p.ListModels(context.Background())
	want := ModelInfo{Name: "gemma4:12b", Size: 7_600_000_000, Family: "gemma4", Params: "12B"}
	if err != nil || len(infos) != 1 || infos[0] != want {
		t.Fatalf("ListModels %+v %v", infos, err)
	}
	if err := p.UnloadModel(context.Background(), "gemma4:12b"); err != nil || srv.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("UnloadModel %v %v", err, srv.Unloaded)
	}
}

func TestTaraengineProviderReportsAFailingList(t *testing.T) {
	p := NewTaraengineProvider("http://127.0.0.1:1", "")
	if _, err := p.DetectModels(context.Background()); err == nil {
		t.Error("DetectModels: no error")
	}
	if _, err := p.ListModels(context.Background()); err == nil {
		t.Error("ListModels: no error")
	}
}
