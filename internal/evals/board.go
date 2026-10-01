package evals

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// HardwareBoard is the ranking on one machine: every model whose newest results carry that hardware
// label. A model the engine reported as not entirely on the GPU is listed apart, since the board's
// promise is "runs entirely on this GPU"; a result with no engine block stays in the ranking with
// empty memory columns.
type HardwareBoard struct {
	Hardware  string `json:"hardware"`
	Rows      []Row  `json:"rows"`
	DidNotFit []Row  `json:"did_not_fit,omitempty"`
}

// buildBoards groups the rows that carry a hardware label into one board per label: boards by label,
// rows by rankLess.
func buildBoards(rows []Row) []HardwareBoard {
	byHardware := map[string]*HardwareBoard{}
	for _, r := range rows {
		if r.Hardware == "" {
			continue
		}
		b, ok := byHardware[r.Hardware]
		if !ok {
			b = &HardwareBoard{Hardware: r.Hardware, Rows: []Row{}}
			byHardware[r.Hardware] = b
		}
		if r.GPUPercent != nil && *r.GPUPercent < 100 {
			b.DidNotFit = append(b.DidNotFit, r)
			continue
		}
		b.Rows = append(b.Rows, r)
	}
	boards := make([]HardwareBoard, 0, len(byHardware))
	for _, b := range byHardware {
		sort.Slice(b.Rows, func(i, j int) bool { return rankLess(b.Rows[i], b.Rows[j]) })
		sort.Slice(b.DidNotFit, func(i, j int) bool { return rankLess(b.DidNotFit[i], b.DidNotFit[j]) })
		boards = append(boards, *b)
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].Hardware < boards[j].Hardware })
	return boards
}

// rankLess orders a board: pass rate, then mean score, then tokens per second, each descending, then
// the model name.
func rankLess(a, b Row) bool {
	switch {
	case a.PassRate != b.PassRate:
		return a.PassRate > b.PassRate
	case a.MeanScore != b.MeanScore:
		return a.MeanScore > b.MeanScore
	case a.TokensPerS != b.TokensPerS:
		return a.TokensPerS > b.TokensPerS
	}
	return a.Model < b.Model
}

// markdown renders the board: its facts, the ranking and the models that did not fit.
func (h HardwareBoard) markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n\n", h.Hardware)
	fmt.Fprintf(&b, "%s Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own "+
		"generation rate; VRAM is the loaded model at the context window the run asked for.\n\n", h.facts())
	b.WriteString("| # | Model | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for i, r := range h.Rows {
		fmt.Fprintf(&b, "| %d | %s | %s | %.0f%% | %.2f | %s | %.0f s | %s | %s | %d |\n",
			i+1, r.Model, tierShort(r.Tier), r.PassRate*100, r.MeanScore, numberOrDash(r.TokensPerS, "%.0f", ""),
			r.MeanWallS, numberOrDash(r.VRAMGB, "%.1f", " GB"), textOrDash(r.Quantization), r.Runs)
	}
	if len(h.DidNotFit) > 0 {
		b.WriteString("\nDid not fit entirely on the GPU (not ranked):\n\n")
		for _, r := range h.DidNotFit {
			fmt.Fprintf(&b, "- %s: %.1f GB loaded, %d%% on the GPU, pass rate %.0f%%\n",
				r.Model, r.SizeGB, *r.GPUPercent, r.PassRate*100)
		}
	}
	return b.String()
}

// facts is the board's one-line summary: how many models are ranked, which engine versions and
// context windows their runs used, and whether every row is a single run.
func (h HardwareBoard) facts() string {
	versions, windows := map[string]bool{}, map[string]bool{}
	single := true
	for _, r := range h.Rows {
		if r.Ollama != "" {
			versions[r.Ollama] = true
		}
		if r.ContextLength > 0 {
			windows[strconv.Itoa(r.ContextLength)] = true
		}
		if r.Runs > 1 {
			single = false
		}
	}
	noun := "models"
	if len(h.Rows) == 1 {
		noun = "model"
	}
	parts := []string{fmt.Sprintf("%d %s", len(h.Rows), noun)}
	if v := sortedKeys(versions); len(v) > 0 {
		parts = append(parts, "Ollama "+strings.Join(v, " and "))
	}
	if w := sortedKeys(windows); len(w) > 0 {
		parts = append(parts, "context window "+strings.Join(w, " and "))
	}
	if single {
		parts = append(parts, "one run each")
	} else {
		parts = append(parts, "runs per model in the last column")
	}
	return strings.Join(parts, ", ") + "."
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tierShort is a registry tier as the board's column shows it.
func tierShort(tier string) string {
	switch tier {
	case "16", "32", "48":
		return tier + " GB"
	case "small":
		return "small"
	}
	return "-"
}

// numberOrDash formats a measured value, or a dash when there is none.
func numberOrDash(v float64, format, unit string) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf(format, v) + unit
}

func textOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
