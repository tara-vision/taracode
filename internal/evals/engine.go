package evals

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tara-vision/taracode/internal/provider"
)

// MaxHardwareLabel bounds the hardware label an operator declares for a run.
const MaxHardwareLabel = 60

// EngineInfo is what the engine says about the loaded model once the warm-up has loaded it: the
// memory it occupies at the context window the run asked for and the share of that on the GPU.
type EngineInfo struct {
	SizeBytes     int64  `json:"size_bytes"`
	VRAMBytes     int64  `json:"vram_bytes"`
	GPUPercent    int    `json:"gpu_percent"`
	ContextLength int    `json:"context_length"`
	Quantization  string `json:"quantization,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Digest        string `json:"digest,omitempty"`
}

// CleanHardware trims a run's hardware label and refuses one that is longer than MaxHardwareLabel
// characters or is not a single line of printable characters. The label is the operator's own
// description of the machine ("NVIDIA RTX 5090 (32 GB)"): the engine does not name its GPU. It is
// public, like the host label, so it must never be a host name.
func CleanHardware(label string) (string, error) {
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) > MaxHardwareLabel {
		return "", fmt.Errorf("the hardware label is longer than %d characters", MaxHardwareLabel)
	}
	for _, r := range label {
		if !unicode.IsPrint(r) {
			return "", errors.New("the hardware label must be one line of printable characters")
		}
	}
	return label, nil
}

// engineInfo reads the loaded model's entry from the engine: the entry named as the model was asked
// for, or that name with ":latest" (ruling P3-R74). It fails when the engine does not answer, does
// not list the model as loaded, or reports no size for it.
func engineInfo(ctx context.Context, opts RunOptions) (*EngineInfo, error) {
	prov, err := provider.New(ctx, opts.Host, opts.Vendor, opts.APIKey)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, engineCallTimeout)
	defer cancel()
	loaded, err := prov.LLM().Loaded(callCtx)
	if err != nil {
		return nil, err
	}
	for _, m := range loaded {
		if m.Name != opts.Model && m.Name != opts.Model+":latest" {
			continue
		}
		if m.Size <= 0 {
			return nil, fmt.Errorf("the engine reports no size for %s", m.Name)
		}
		return &EngineInfo{
			SizeBytes: m.Size, VRAMBytes: m.SizeVRAM, GPUPercent: gpuPercent(m.Size, m.SizeVRAM),
			ContextLength: m.ContextLength, Quantization: m.Quantization, ParameterSize: m.ParameterSize,
			Digest: shortDigest(m.Digest),
		}, nil
	}
	return nil, fmt.Errorf("the engine does not list %s as loaded", opts.Model)
}

// loadedEngine is engineInfo for Run. The block is measurement metadata, so a failure is one warning
// on the terminal and never the run's.
func loadedEngine(ctx context.Context, opts RunOptions) *EngineInfo {
	info, err := engineInfo(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Out, "warning: no engine block in the results: %v\n", err)
		return nil
	}
	return info
}

// gpuPercent is the share of the loaded model on the GPU: 100 only when all of it is, otherwise the
// percentage rounded down, so a model that is almost entirely on the GPU never reads as fitting.
func gpuPercent(size, vram int64) int {
	switch {
	case size <= 0:
		return 0
	case vram >= size:
		return 100
	}
	return int(vram * 100 / size)
}

// shortDigest is the first twelve hex characters of a model digest, without the sha256: prefix.
func shortDigest(digest string) string {
	digest = strings.TrimPrefix(digest, "sha256:")
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// generationRate is the completion tokens of every task over the generation time of every task, as
// the engine reported both; 0 when it reported no generation time.
func generationRate(results []TaskResult) float64 {
	var tokens, ms float64
	for _, r := range results {
		tokens += float64(r.CompletionTokens)
		ms += float64(r.EvalMs)
	}
	if ms <= 0 {
		return 0
	}
	return round1(tokens / (ms / 1000))
}

// speedAndMemory is the tail of a run's last line: the generation rate and the loaded model's
// memory, each only when the engine reported it.
func speedAndMemory(res Results) string {
	out := ""
	if res.Summary.TokensPerS > 0 {
		out += fmt.Sprintf(", %.0f tok/s", res.Summary.TokensPerS)
	}
	if e := res.Engine; e != nil {
		out += fmt.Sprintf(", %.1f GB (%d%% GPU)", float64(e.SizeBytes)/1e9, e.GPUPercent)
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
