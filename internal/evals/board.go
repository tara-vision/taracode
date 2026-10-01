package evals

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// HardwareBoard is the ranking on one machine: every model whose newest results carry that hardware
// label and cover the whole corpus. A model the engine reported as not entirely on the GPU is listed
// apart, since the board's promise is "runs entirely on this GPU", and so is a result that ran only
// part of the corpus (a --tasks subset), which is never comparable. A result with no engine block
// stays in the ranking with empty memory columns.
type HardwareBoard struct {
	Hardware  string `json:"hardware"`
	Rows      []Row  `json:"rows"`
	DidNotFit []Row  `json:"did_not_fit,omitempty"`
	Partial   []Row  `json:"partial,omitempty"`
}

// Unchecked counts the ranked rows the engine described nothing for: their fit was never checked.
func (h HardwareBoard) Unchecked() int {
	n := 0
	for _, r := range h.Rows {
		if r.GPUPercent == nil {
			n++
		}
	}
	return n
}

// buildBoards groups the rows that carry a hardware label into one board per label: boards by label,
// rows by rankLess. corpusTasks is the size of the corpus; 0 turns the whole-corpus rule off.
func buildBoards(rows []Row, corpusTasks int) []HardwareBoard {
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
		switch {
		case corpusTasks > 0 && r.Tasks != corpusTasks:
			b.Partial = append(b.Partial, r)
		case r.GPUPercent != nil && *r.GPUPercent < 100:
			b.DidNotFit = append(b.DidNotFit, r)
		default:
			b.Rows = append(b.Rows, r)
		}
	}
	boards := make([]HardwareBoard, 0, len(byHardware))
	for _, b := range byHardware {
		for _, list := range [][]Row{b.Rows, b.DidNotFit, b.Partial} {
			sort.Slice(list, func(i, j int) bool { return rankLess(list[i], list[j]) })
		}
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

// markdown renders the board: its facts and the ranking, then the models that did not fit and the
// runs that covered only part of the corpus. Every string from a results file goes through mdText.
func (h HardwareBoard) markdown(corpusTasks int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n\n", mdText(h.Hardware))
	if len(h.Rows) > 0 {
		fmt.Fprintf(&b, "%s Ranked by pass rate, then mean score, then tokens per second. Tokens/s is the engine's own "+
			"generation rate; VRAM is the loaded model at the context window the run asked for.\n\n", h.facts())
		b.WriteString("| # | Model | Tier | Pass rate | Mean score | Tokens/s | Mean wall | VRAM | Quant | Runs |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
		for i, r := range h.Rows {
			fmt.Fprintf(&b, "| %d | %s | %s | %.0f%% | %.2f | %s | %.0f s | %s | %s | %d |\n",
				i+1, mdText(r.Model), tierShort(r.Tier), r.PassRate*100, r.MeanScore, numberOrDash(r.TokensPerS, "%.0f", ""),
				r.MeanWallS, numberOrDash(r.VRAMGB, "%.1f", " GB"), textOrDash(mdText(r.Quantization)), r.Runs)
		}
	} else if len(h.DidNotFit) > 0 {
		b.WriteString("No model ran entirely on this GPU.\n")
	}
	if len(h.DidNotFit) > 0 {
		b.WriteString("\nDid not fit entirely on the GPU (not ranked):\n\n")
		for _, r := range h.DidNotFit {
			pct := 0
			if r.GPUPercent != nil {
				pct = *r.GPUPercent
			}
			fmt.Fprintf(&b, "- %s: %.1f GB loaded, %d%% on the GPU, pass rate %.0f%%\n",
				mdText(r.Model), r.SizeGB, pct, r.PassRate*100)
		}
	}
	if len(h.Partial) > 0 {
		b.WriteString("\nNot ranked, ran only part of the corpus:\n\n")
		for _, r := range h.Partial {
			fmt.Fprintf(&b, "- %s: %d of %d tasks\n", mdText(r.Model), r.Tasks, corpusTasks)
		}
	}
	return b.String()
}

// facts is the board's one-line summary: how many models are ranked, which engine versions and
// context windows their runs used, and whether every row is a single run.
func (h HardwareBoard) facts() string {
	versions, windows := map[string]bool{}, map[int]bool{}
	single := true
	for _, r := range h.Rows {
		if r.Ollama != "" {
			versions[r.Ollama] = true
		}
		if r.ContextLength > 0 {
			windows[r.ContextLength] = true
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
	if len(versions) > 0 {
		names := make([]string, 0, len(versions))
		for v := range versions {
			names = append(names, mdText(v))
		}
		sort.Strings(names)
		parts = append(parts, "Ollama "+joinList(names))
	}
	if len(windows) > 0 {
		sizes := make([]int, 0, len(windows))
		for w := range windows {
			sizes = append(sizes, w)
		}
		sort.Ints(sizes)
		names := make([]string, 0, len(sizes))
		for _, w := range sizes {
			names = append(names, strconv.Itoa(w))
		}
		parts = append(parts, "context window "+joinList(names))
	}
	if single {
		parts = append(parts, "one run each")
	} else {
		parts = append(parts, "runs per model in the last column")
	}
	return strings.Join(parts, ", ") + "."
}

// joinList writes a list the way a sentence does: "a", "a and b", "a, b and c".
func joinList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// mdText makes a string from a results file safe inside a Markdown table cell or heading: a pipe is
// escaped and a line break becomes a space, so a file cannot split a row or start a block.
func mdText(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "|", `\|`).Replace(s)
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
