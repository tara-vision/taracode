package agent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/ui"
)

// TestSameModel is a direct table test of sameModel's tolerance for Ollama's implicit ":latest"
// tag: /api/tags lists an untagged model (e.g. "glm-4.7-flash") as "glm-4.7-flash:latest", so a
// bare configured or persisted name must be recognized as the same model as its tagged listing,
// and vice versa. Only one trailing ":latest" is stripped from each side, so a name that already
// carries the suffix twice is not collapsed any further.
func TestSameModel(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"bare name vs the engine's :latest listing", "glm-4.7-flash", "glm-4.7-flash:latest", true},
		{":latest listing vs a bare name", "glm-4.7-flash:latest", "glm-4.7-flash", true},
		{"identical bare names", "qwen3.5:9b", "qwen3.5:9b", true},
		{"identical :latest-tagged names", "foo:latest", "foo:latest", true},
		{"a real tag is not :latest, so it is a different model", "gemma4:12b", "gemma4", false},
		{"different bare names", "gemma4", "qwen3.5", false},
		{"double :latest suffix strips only once, so it does not equal the single-tagged form", "x:latest:latest", "x:latest", false},
		{"double :latest suffix does not collapse all the way to the bare name", "x:latest:latest", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameModel(tt.a, tt.b); got != tt.want {
				t.Errorf("sameModel(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			// sameModel must not depend on argument order.
			if got := sameModel(tt.b, tt.a); got != tt.want {
				t.Errorf("sameModel(%q, %q) = %v, want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

// TestChooseModelToleratesOllamasImplicitLatestTag covers the fix for a silent model substitution
// bug: Ollama treats a model named without a tag as "name:latest" and lists it that way from
// /api/tags, but taracode's registry and docs name models without a tag. Before this fix,
// modelAvailable compared the persisted or configured name against the engine's list with an exact
// string match, so a bare "--model glm-4.7-flash" never matched the listed "glm-4.7-flash:latest";
// chooseModel then silently fell back to the first listed model with only a warning, so the
// assistant could run a completely different model than the one asked for. Each case checks both
// the resolved model name and the message chooseModel printed, since a right model chosen for the
// wrong reason (or a wrong model chosen quietly) would still be a bug.
func TestChooseModelToleratesOllamasImplicitLatestTag(t *testing.T) {
	tests := []struct {
		name           string
		models         []string
		persistedModel string
		configModel    string
		wantModel      string
		wantContains   string
		wantAbsent     string // a phrase that must NOT appear, guarding against the silent-fallback path
	}{
		{
			name:         "configured bare name matches the engine's :latest listing",
			models:       []string{"nemotron-3.5-lightning:30b", "glm-4.7-flash:latest"},
			configModel:  "glm-4.7-flash",
			wantModel:    "glm-4.7-flash",
			wantContains: "Using configured model: glm-4.7-flash",
			wantAbsent:   "not available",
		},
		{
			name:           "persisted exact match is unchanged",
			models:         []string{"qwen3.5:9b"},
			persistedModel: "qwen3.5:9b",
			wantModel:      "qwen3.5:9b",
			wantContains:   "Using saved model: qwen3.5:9b",
		},
		{
			name:           "persisted :latest-tagged name matches the engine's bare listing",
			models:         []string{"foo"},
			persistedModel: "foo:latest",
			wantModel:      "foo:latest",
			wantContains:   "Using saved model: foo:latest",
		},
		{
			// A real, different tag must still fall back: this is not the bug, it is a genuinely
			// different model, and the fix must not blur that distinction.
			name:         "configured name with a different (non-latest) tag is genuinely unavailable",
			models:       []string{"gemma4:12b"},
			configModel:  "gemma4",
			wantModel:    "gemma4:12b",
			wantContains: "Configured model 'gemma4' not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			renderer := ui.NewRenderer()

			model, err := chooseModel(tt.models, nil, tt.persistedModel, tt.configModel, renderer, &out)
			if err != nil {
				t.Fatalf("chooseModel() error = %v", err)
			}
			if model != tt.wantModel {
				t.Errorf("chooseModel() model = %q, want %q", model, tt.wantModel)
			}
			if tt.wantContains != "" && !strings.Contains(out.String(), tt.wantContains) {
				t.Errorf("chooseModel() output = %q, want it to contain %q", out.String(), tt.wantContains)
			}
			if tt.wantAbsent != "" && strings.Contains(out.String(), tt.wantAbsent) {
				t.Errorf("chooseModel() output = %q, want it to NOT contain %q", out.String(), tt.wantAbsent)
			}
		})
	}
}
