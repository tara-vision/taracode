package agent

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/models"
	"github.com/tara-vision/taracode/internal/ui"
)

// TestChooseModelFallsBackInOrder walks every branch of the model choice: a server that cannot
// list its models trusts the saved name, then the configured one; a server that lists models
// replaces a saved name it no longer has with its first model; a server that lists none trusts
// whatever name it was given.
func TestChooseModelFallsBackInOrder(t *testing.T) {
	stubFirstRun(t, func() (int, error) { return 0, errors.New("unsized") }, models.Load)
	detect := errors.New("connection refused")
	listed := []string{"gemma4:12b", "qwen3.5:9b"}
	tests := []struct {
		name                 string
		models               []string
		detectErr            error
		persisted, configure string
		want, wantOut        string
		wantErr              string
	}{
		{"detection failed, saved", nil, detect, "saved:1b", "conf:2b", "saved:1b", "Using saved model: saved:1b", ""},
		{"detection failed, configured", nil, detect, "", "conf:2b", "conf:2b",
			"Could not detect models (connection refused), using configured: conf:2b", ""},
		{"detection failed, nothing to fall back on", nil, detect, "", "", "", "",
			"failed to detect models and no fallback configured: connection refused"},
		{"saved model gone", listed, nil, "gone:1b", "qwen3.5:9b", "gemma4:12b",
			"Saved model 'gone:1b' not available. Use /model to select.", ""},
		{"nothing asked for", listed, nil, "", "", "gemma4:12b", "Using model: gemma4:12b", ""},
		{"empty list, saved", []string{}, nil, "saved:1b", "conf:2b", "saved:1b", "", ""},
		{"empty list, configured", []string{}, nil, "", "conf:2b", "conf:2b", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := chooseModel(tt.models, tt.detectErr, tt.persisted, tt.configure, ui.NewRenderer(), &out)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr || !errors.Is(err, detect) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("chooseModel() = %q, %v; want %q", got, err, tt.want)
			}
			if !strings.Contains(out.String(), tt.wantOut) || (tt.wantOut == "" && out.Len() != 0) {
				t.Fatalf("output %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

// TestChooseModelSaysWhichModelReplacesAGoneOne: the warning about a saved model the server no
// longer lists is followed by the model used instead.
func TestChooseModelSaysWhichModelReplacesAGoneOne(t *testing.T) {
	var out bytes.Buffer
	if _, err := chooseModel([]string{"gemma4:12b"}, nil, "gone:1b", "", ui.NewRenderer(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Using: gemma4:12b") {
		t.Fatalf("%q", out.String())
	}
}

// stubFirstRun replaces how printFirstRunAdvice sizes the host and loads the registry.
func stubFirstRun(t *testing.T, ram func() (int, error), load func() (*models.Registry, error)) {
	t.Helper()
	oldRAM, oldLoad := hostRAMGB, loadRegistry
	hostRAMGB, loadRegistry = ram, load
	t.Cleanup(func() { hostRAMGB, loadRegistry = oldRAM, oldLoad })
}

func TestFirstRunAdviceNamesTheTierDefault(t *testing.T) {
	registry := &models.Registry{Entries: []models.Entry{
		{Name: "small:4b", Tier: models.Tier16, Default: true, DownloadGB: 3},
		{Name: "big:70b", Tier: models.Tier48, DownloadGB: 40.4},
	}}
	stubFirstRun(t, func() (int, error) { return 64, nil }, func() (*models.Registry, error) { return registry, nil })
	var out bytes.Buffer
	printFirstRunAdvice(&out)
	if want := "Advice    recommended for 64 GB: big:70b (40 GB download)\n          ollama pull big:70b\n"; out.String() != want {
		t.Fatalf("advice %q, want %q", out.String(), want)
	}
}

// TestFirstRunAdviceIsSilentWhenItCannotAdvise: a host it cannot size, a registry it cannot load
// and a tier with no entry each leave the advice out instead of failing.
func TestFirstRunAdviceIsSilentWhenItCannotAdvise(t *testing.T) {
	sized := func() (int, error) { return 64, nil }
	onlySmall := func() (*models.Registry, error) {
		return &models.Registry{Entries: []models.Entry{{Name: "small:4b", Tier: models.Tier16, Default: true}}}, nil
	}
	tests := []struct {
		name string
		ram  func() (int, error)
		load func() (*models.Registry, error)
	}{
		{"unsized host", func() (int, error) { return 0, errors.New("no sysctl") }, models.Load},
		{"broken registry", sized, func() (*models.Registry, error) { return nil, errors.New("bad yaml") }},
		{"no entry for the tier", sized, onlySmall},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubFirstRun(t, tt.ram, tt.load)
			var out bytes.Buffer
			printFirstRunAdvice(&out)
			if out.Len() != 0 {
				t.Fatalf("printed %q", out.String())
			}
		})
	}
}
