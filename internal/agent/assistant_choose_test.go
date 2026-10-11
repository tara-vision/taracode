package agent

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
	"github.com/tara-vision/taracode/internal/ui"
)

// TestChooseModelFallsBackInOrder walks every branch of the model choice: a server that cannot
// list its models trusts the saved name, then the configured one; a server that lists models
// replaces a saved name it no longer has with its first model; a server that lists none trusts
// whatever name it was given.
func TestChooseModelFallsBackInOrder(t *testing.T) {
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
			got, err := chooseModel(tt.models, tt.detectErr, tt.persisted, tt.configure, ui.NewRenderer(), &out, nil)
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
	if _, err := chooseModel([]string{"gemma4:12b"}, nil, "gone:1b", "", ui.NewRenderer(), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Using: gemma4:12b") {
		t.Fatalf("%q", out.String())
	}
}

// TestChooseModelSaysHowToPullAConfiguredModelOnTheEngine: a configured model the server does not list gives way to
// the first listed one; on the engine a second line says how to pull it, and elsewhere nothing more is said.
func TestChooseModelSaysHowToPullAConfiguredModelOnTheEngine(t *testing.T) {
	srv := taraenginetest.New(t)
	for _, c := range []struct {
		name   string
		client llm.Client
		pull   bool
	}{{"the engine", taraengine.New(srv.URL, "", nil), true}, {"another server", nil, false}} {
		var out bytes.Buffer
		got, err := chooseModel([]string{"gemma4:12b"}, nil, "", "qwen3.6:35b", ui.NewRenderer(), &out, c.client)
		if err != nil || got != "gemma4:12b" ||
			!strings.Contains(out.String(), "Configured model 'qwen3.6:35b' not available. Using: gemma4:12b") ||
			strings.Contains(out.String(), "Pull it with: taraengine pull qwen3.6:35b") != c.pull {
			t.Errorf("%s: %q %v, output %q", c.name, got, err, out.String())
		}
	}
}

func TestFirstRunAdviceNamesTheEnginesRecommendation(t *testing.T) {
	srv := taraenginetest.New(t)
	var out bytes.Buffer
	printFirstRunAdvice(&out, taraengine.New(srv.URL, "", nil))
	if out.String() != "Advice    recommended for tier 32: glm-4.7-flash\n          taraengine pull glm-4.7-flash\n" {
		t.Fatalf("advice %q", out.String())
	}
}

func TestFirstRunAdviceStaysSilentWhenItCannotSay(t *testing.T) {
	broken := taraenginetest.New(t)
	broken.Failures["GET /api/doctor"] = &taraenginetest.Failure{Status: 500, Message: "x"}
	empty := taraenginetest.New(t)
	empty.Doctor.Recommendation = map[string]string{}
	clients := map[string]llm.Client{"not the engine": nil, "a failing doctor": taraengine.New(broken.URL, "", nil),
		"no recommendation": taraengine.New(empty.URL, "", nil)}
	for name, client := range clients {
		var out bytes.Buffer
		printFirstRunAdvice(&out, client)
		if out.Len() != 0 {
			t.Errorf("%s: %q", name, out.String())
		}
	}
}
