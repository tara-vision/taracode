package evals

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
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

// CleanHardware trims a run's hardware label, collapses its inner runs of spaces (so one machine is
// one board), and refuses a label that is not valid UTF-8, is not a single line of printable
// characters, is longer than MaxHardwareLabel characters, or carries a URL or an IP address. The
// label is the operator's own description of the machine ("NVIDIA RTX 5090 (32 GB)"): the engine
// does not name its GPU. It is public, like the host label, so it describes the machine and never
// addresses it.
func CleanHardware(label string) (string, error) {
	if !utf8.ValidString(label) {
		return "", errors.New("the hardware label is not valid UTF-8")
	}
	label = strings.TrimSpace(label)
	for _, r := range label {
		if !unicode.IsPrint(r) {
			return "", errors.New("the hardware label must be one line of printable characters")
		}
	}
	label = strings.Join(strings.Fields(label), " ")
	if utf8.RuneCountInString(label) > MaxHardwareLabel {
		return "", fmt.Errorf("the hardware label is longer than %d characters", MaxHardwareLabel)
	}
	if strings.Contains(label, "://") || ipLiteral.MatchString(label) {
		return "", errors.New("the hardware label describes the machine, it must not address it: no URL, no IP address")
	}
	return label, nil
}

// engineToken is what a quantization or a parameter size may look like in a results file. These
// strings come from the engine and are published, so one that looks like anything else is dropped
// (ruling P3-R45: a results file never carries an engine address or a host path).
var engineToken = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// hexDigest is a model digest once the sha256: prefix is gone.
var hexDigest = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// cleanToken is s when it is a plain engine token, else "".
func cleanToken(s string) string {
	if engineToken.MatchString(s) {
		return s
	}
	return ""
}

// engineInfo reads the loaded model's entry from the engine: the entry named as the model was asked
// for, or that name with ":latest" (ruling P3-R74). It fails when the engine does not answer, does
// not list the model as loaded, or reports no size for it. others is how many other models the
// engine holds on the GPU, -1 when the engine did not say: a GPU measurement would include them. A
// model the engine keeps entirely in system memory takes nothing on the GPU and is not counted.
func engineInfo(ctx context.Context, opts RunOptions) (info *EngineInfo, others int, err error) {
	prov, err := provider.New(ctx, opts.Host, opts.Vendor, opts.APIKey)
	if err != nil {
		return nil, -1, err
	}
	callCtx, cancel := context.WithTimeout(ctx, engineCallTimeout)
	defer cancel()
	loaded, err := prov.LLM().Loaded(callCtx)
	if err != nil {
		return nil, -1, err
	}
	err = fmt.Errorf("the engine does not list %s as loaded (if it has another name there, use the one "+
		"`ollama list` prints)", opts.Model)
	for _, m := range loaded {
		switch {
		case m.Name != opts.Model && m.Name != opts.Model+":latest":
			if m.SizeVRAM > 0 {
				others++
			}
		case m.Size <= 0:
			err = fmt.Errorf("the engine reports no size for %s", m.Name)
		default:
			info, err = &EngineInfo{
				SizeBytes: m.Size, VRAMBytes: m.SizeVRAM, GPUPercent: gpuPercent(m.Size, m.SizeVRAM),
				ContextLength: m.ContextLength, Quantization: cleanToken(m.Quantization),
				ParameterSize: cleanToken(m.ParameterSize), Digest: shortDigest(m.Digest),
			}, nil
		}
	}
	return info, others, err
}

// loadedEngine is engineInfo for Run. The block is measurement metadata, so a failure is one warning
// on the terminal and never the run's.
func loadedEngine(ctx context.Context, opts RunOptions) (info *EngineInfo, others int) {
	info, others, err := engineInfo(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Out, "warning: no engine block in the results: %v\n", err)
		return nil, others
	}
	return info, others
}

// gpuPercent is the share of the loaded model on the GPU: 100 only when all of it is, otherwise the
// percentage rounded down, so a model that is almost entirely on the GPU never reads as fitting.
func gpuPercent(size, vram int64) int {
	switch {
	case size <= 0 || vram <= 0:
		return 0
	case vram >= size:
		return 100
	}
	return int(vram * 100 / size)
}

// shortDigest is the first twelve hex characters of a model digest, without the sha256: prefix; ""
// when what the engine sent is not hex.
func shortDigest(digest string) string {
	digest = strings.TrimPrefix(digest, "sha256:")
	if !hexDigest.MatchString(digest) {
		return ""
	}
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// generationRate is the completion tokens over the generation time of every task the engine timed,
// as it reported both; 0 when it timed none. A task with tokens and no time is left out, so its
// tokens are never divided by the other tasks' time.
func generationRate(results []TaskResult) float64 {
	var tokens, ms float64
	for _, r := range results {
		if r.EvalMs <= 0 {
			continue
		}
		tokens += float64(r.CompletionTokens)
		ms += float64(r.EvalMs)
	}
	if ms <= 0 {
		return 0
	}
	return round1(tokens / (ms / 1000))
}

// speedAndMemory is the tail of a run's last line: the generation rate, the GPU memory the probe
// measured and the loaded model's memory as the engine reports it, each only when there is one.
func speedAndMemory(res Results) string {
	out := ""
	if res.Summary.TokensPerS > 0 {
		out += fmt.Sprintf(", %.0f tok/s", res.Summary.TokensPerS)
	}
	e := res.Engine
	switch {
	case res.GPUMemoryMiB > 0 && e != nil:
		out += fmt.Sprintf(", %.1f GiB on the GPU (engine reports %.1f GB, %d%% GPU)",
			gib(res.GPUMemoryMiB), float64(e.SizeBytes)/1e9, e.GPUPercent)
	case res.GPUMemoryMiB > 0:
		out += fmt.Sprintf(", %.1f GiB on the GPU", gib(res.GPUMemoryMiB))
	case e != nil:
		out += fmt.Sprintf(", %.1f GB (%d%% GPU)", float64(e.SizeBytes)/1e9, e.GPUPercent)
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
