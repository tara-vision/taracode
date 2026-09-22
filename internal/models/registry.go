// Package models is the embedded registry of recommended Ollama models and the host RAM tiers.
package models

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed registry.yaml
var registryYAML []byte

// Tier is a host RAM class.
type Tier string

// The supported RAM tiers, from smallest to largest.
const (
	// TierSmall fits any host; its entries are helper-sized models, not tier defaults.
	TierSmall Tier = "small"
	// Tier16 is the floor tier: it covers any host under 32 GB of RAM, not just hosts with at
	// least 16 GB.
	Tier16 Tier = "16"
	// Tier32 is the class for hosts with at least 32 GB of RAM.
	Tier32 Tier = "32"
	// Tier48 is the class for hosts with 48 GB of RAM or more.
	Tier48 Tier = "48"
)

// Entry is one recommended model.
type Entry struct {
	Name         string   `yaml:"name"`         // Ollama pull name; the tag is optional (glm-4.7-flash has none).
	Family       string   `yaml:"family"`       // model family; used to find an entry when the tag is omitted.
	Params       string   `yaml:"params"`       // parameter count as published upstream.
	DownloadGB   float64  `yaml:"download_gb"`  // approximate download size, in gigabytes, for a Q4-class quantization.
	Context      int      `yaml:"context"`      // context window, in tokens.
	Capabilities []string `yaml:"capabilities"` // supported capabilities, such as tool calling or vision input.
	Tier         Tier     `yaml:"tier"`         // the RAM tier this entry belongs to.
	Default      bool     `yaml:"default"`      // true marks the recommended entry for its tier.
	MinOllama    string   `yaml:"min_ollama"`   // first Ollama release shipping this model's tool-call parser.
	Notes        string   `yaml:"notes"`        // free-text rationale for the recommendation.
	EvalScore    float64  `yaml:"eval_score"`   // reserved for the Phase 3 evaluation scoreboard; empty until then.
}

// Registry is the loaded registry.
type Registry struct {
	Entries []Entry `yaml:"entries"` // every known model, across all tiers.
}

// Load parses the embedded registry.
func Load() (*Registry, error) {
	var r Registry
	if err := yaml.Unmarshal(registryYAML, &r); err != nil {
		return nil, fmt.Errorf("models: parse registry: %w", err)
	}
	return &r, nil
}

// Find looks a model up by exact name, by name without tag, or by family.
func (r *Registry) Find(name string) (Entry, bool) {
	base := strings.SplitN(name, ":", 2)[0]
	for _, e := range r.Entries {
		if e.Name == name {
			return e, true
		}
	}
	for _, e := range r.Entries {
		if e.Family == base || strings.SplitN(e.Name, ":", 2)[0] == base {
			return e, true
		}
	}
	return Entry{}, false
}

// DefaultForTier returns the entry marked default for a tier (the first entry of the tier otherwise).
func (r *Registry) DefaultForTier(t Tier) Entry {
	var first *Entry
	for i := range r.Entries {
		e := &r.Entries[i]
		if e.Tier != t {
			continue
		}
		if e.Default {
			return *e
		}
		if first == nil {
			first = e
		}
	}
	if first != nil {
		return *first
	}
	return Entry{}
}

// DefaultName is the registry default for a tier, or "" when the registry cannot be loaded.
func DefaultName(t Tier) string {
	r, err := Load()
	if err != nil {
		return ""
	}
	return r.DefaultForTier(t).Name
}

// TierFor maps host RAM in GB to a tier.
func TierFor(ramGB int) Tier {
	switch {
	case ramGB >= 48:
		return Tier48
	case ramGB >= 32:
		return Tier32
	default:
		return Tier16
	}
}

// Recommend lists the entries that fit the host, default first, then by download size.
func (r *Registry) Recommend(ramGB int) []Entry {
	tier := TierFor(ramGB)
	var out []Entry
	for _, e := range r.Entries {
		if e.Tier == tier {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].DownloadGB < out[j].DownloadGB
	})
	return out
}
