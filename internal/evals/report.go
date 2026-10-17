package evals

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

// Scoreboard is the report (spec 10): the newest results per model, server and machine, ranked per hardware label
// and grouped by tier.
type Scoreboard struct {
	GeneratedAt string          `json:"generated_at"`
	Taracode    string          `json:"taracode"`
	CorpusTasks int             `json:"corpus_tasks"`
	Boards      []HardwareBoard `json:"boards,omitempty"` // one ranking per hardware label the results carry
	Tiers       []TierBoard     `json:"tiers"`
	Skipped     []string        `json:"skipped,omitempty"` // results left out and why
}

// TierBoard is one tier's rows, best mean score first.
type TierBoard struct {
	Tier string `json:"tier"`
	Rows []Row  `json:"rows"`
}

// Row is one model's line on one server and machine. Tier, Runs, Tasks, SuiteWallS and Server are set for every result,
// so the JSON of a board built from older results gains those keys too (readers pick keys by name). Hardware,
// the rate and the memory fields are empty for a result that has no label, no timings or no engine block,
// and are then left out of the JSON.
type Row struct {
	Model           string                 `json:"model"`
	Default         bool                   `json:"default"`
	Tier            string                 `json:"tier,omitempty"`
	PassRate        float64                `json:"pass_rate"`
	MeanScore       float64                `json:"mean_score"`
	MeanIterations  float64                `json:"mean_iterations"`
	MeanWallS       float64                `json:"mean_wall_s"`
	FixtureMissRate float64                `json:"fixture_miss_rate"`
	ByArea          map[string]AreaSummary `json:"by_area"`
	Taracode        string                 `json:"taracode"`
	Server          ServerInfo             `json:"server"`
	Ollama          string                 `json:"ollama,omitempty"` // the Ollama version, on rows from Ollama results
	Think           string                 `json:"think"`
	Date            string                 `json:"date"`
	Runs            int                    `json:"runs,omitempty"`
	Tasks           int                    `json:"tasks,omitempty"` // tasks the result covers
	Hardware        string                 `json:"hardware,omitempty"`
	TokensPerS      float64                `json:"tokens_per_s,omitempty"`
	SuiteWallS      float64                `json:"suite_wall_s,omitempty"`   // the task wall times added up
	SizeGB          float64                `json:"size_gb,omitempty"`        // the loaded model, decimal gigabytes
	VRAMGB          float64                `json:"vram_gb,omitempty"`        // the part of it on the GPU
	GPUMemoryMiB    int64                  `json:"gpu_memory_mib,omitempty"` // measured on the machine; 0 = not measured
	GPUPercent      *int                   `json:"gpu_percent,omitempty"`    // nil when the engine reported nothing
	ContextLength   int                    `json:"context_length,omitempty"`
	Quantization    string                 `json:"quantization,omitempty"`
	ParameterSize   string                 `json:"parameter_size,omitempty"`
	Digest          string                 `json:"digest,omitempty"`
}

// newRow is one model's line on a server from its results: the summary, the run's header, the measured GPU
// memory (the engine's own measurement or the operator's probe) and, when the engine reported the loaded
// model, its memory figure in decimal gigabytes.
func newRow(r Results, tier string, isDefault bool) Row {
	byArea := map[string]AreaSummary{}
	if r.Summary.ByArea != nil {
		byArea = maps.Clone(r.Summary.ByArea)
	}
	row := Row{
		Model: r.Model, Default: isDefault, Tier: tier,
		PassRate: r.Summary.PassRate, MeanScore: r.Summary.MeanScore, MeanIterations: r.Summary.MeanIterations,
		MeanWallS:       round3(r.Summary.MeanWallMs / 1000),
		FixtureMissRate: r.Summary.FixtureMissRate,
		ByArea:          byArea,
		Taracode:        r.Taracode, Server: r.ServerOf(), Ollama: r.Ollama, Think: r.Think, Date: r.Date,
		Runs: r.Runs, Tasks: len(r.Tasks), Hardware: r.Hardware, TokensPerS: r.Summary.TokensPerS,
		SuiteWallS:   round1(r.Summary.MeanWallMs * float64(len(r.Tasks)) / 1000),
		GPUMemoryMiB: r.GPUMemoryMiB,
	}
	if e := r.Engine; e != nil {
		pct := e.GPUPercent
		row.SizeGB, row.VRAMGB, row.GPUPercent = round1(float64(e.SizeBytes)/1e9), round1(float64(e.VRAMBytes)/1e9), &pct
		row.ContextLength, row.Quantization = e.ContextLength, e.Quantization
		row.ParameterSize, row.Digest = e.ParameterSize, e.Digest
	}
	return row
}

// tierOrder is the display order; unknown tiers go last.
var tierOrder = map[string]int{"16": 0, "32": 1, "48": 2, "small": 3, "other": 4}

// areaOrder is the column order of the Markdown table.
var areaOrder = []Area{AreaKubernetes, AreaHelm, AreaTerraform, AreaDocker, AreaSecrets, AreaCloud, AreaRefusal}

// rowKey is what makes a row: a model on one server on one machine.
type rowKey struct{ model, server, hardware string }

// BuildScoreboard keeps the newest results per model, server and machine, drops any with a safety failure, and groups
// the rest by tier. A model is its tier's default when any of its newest results says so.
func BuildScoreboard(all []Results, corpusTasks int, version string) Scoreboard {
	sb := Scoreboard{
		GeneratedAt: time.Now().Format("2006-01-02"),
		Taracode:    version,
		CorpusTasks: corpusTasks,
		Tiers:       []TierBoard{},
	}
	newest := map[rowKey]Results{}
	for _, r := range all {
		if r.Summary.SafetyFailures > 0 {
			name := r.Model
			if server := r.ServerOf().String(); server != "" {
				name += " on " + server
			}
			if r.Hardware != "" {
				name += ", " + r.Hardware
			}
			msg := fmt.Sprintf("%s %s: %d safety failure(s)", name, r.Date, r.Summary.SafetyFailures)
			sb.Skipped = append(sb.Skipped, msg)
			continue
		}
		key := rowKey{r.Model, r.ServerOf().Name, r.Hardware}
		if cur, ok := newest[key]; !ok || r.Date > cur.Date {
			newest[key] = r
		}
	}
	newest = newestDecides(newest)
	// The default flag is what the engine's catalog said when the run was made, so it is shared across servers but
	// kept per machine: a machine run before a catalog change keeps the default it had.
	type machineModel struct{ model, hardware string }
	defaults := map[machineModel]bool{}
	for _, r := range newest {
		k := machineModel{r.Model, r.Hardware}
		defaults[k] = defaults[k] || r.Default
	}
	byTier := map[string][]Row{}
	var allRows []Row
	for _, r := range newest {
		tier := r.Tier
		if tier == "" {
			tier = "other"
		}
		row := newRow(r, tier, defaults[machineModel{r.Model, r.Hardware}])
		byTier[tier] = append(byTier[tier], row)
		allRows = append(allRows, row)
	}
	sb.Boards = buildBoards(allRows, corpusTasks)
	for tier, rows := range byTier {
		sort.Slice(rows, func(i, j int) bool {
			if a, b := rankScore(rows[i]), rankScore(rows[j]); a != b {
				return a > b
			}
			if rows[i].Model != rows[j].Model {
				return rows[i].Model < rows[j].Model
			}
			if rows[i].Server.Name != rows[j].Server.Name {
				return rows[i].Server.Name < rows[j].Server.Name
			}
			return rows[i].Hardware < rows[j].Hardware
		})
		sb.Tiers = append(sb.Tiers, TierBoard{Tier: tier, Rows: rows})
	}
	sort.Slice(sb.Tiers, func(i, j int) bool {
		ri, rj := tierRank(sb.Tiers[i].Tier), tierRank(sb.Tiers[j].Tier)
		if ri != rj {
			return ri < rj
		}
		return sb.Tiers[i].Tier < sb.Tiers[j].Tier
	})
	sort.Strings(sb.Skipped)
	return sb
}

// newestDecides keeps the rows that a model's newest result on a server allows. When that result names no machine, it
// is the model's only row, so a newer unlabelled run takes the model off the boards instead of leaving a stale row;
// when it names one, every machine keeps its own newest result and the unlabelled ones drop out. On the same date a
// labelled result counts as the newer.
func newestDecides(rows map[rowKey]Results) map[rowKey]Results {
	latest := map[rowKey]Results{}
	for k, r := range rows {
		m := rowKey{k.model, k.server, ""}
		cur, ok := latest[m]
		if !ok || r.Date > cur.Date || (r.Date == cur.Date && cur.Hardware == "" && r.Hardware != "") {
			latest[m] = r
		}
	}
	out := map[rowKey]Results{}
	for k, r := range rows {
		if (latest[rowKey{k.model, k.server, ""}].Hardware == "") == (k.hardware == "") {
			out[k] = r
		}
	}
	return out
}

func tierRank(t string) int {
	if r, ok := tierOrder[t]; ok {
		return r
	}
	return 9
}

// JSON renders scoreboard.json.
func (s Scoreboard) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// tierTitle is the heading of a tier.
func tierTitle(tier string) string {
	switch tier {
	case "16", "32", "48":
		return tier + " GB tier"
	case "small":
		return "Small models"
	}
	return "Other models"
}

// Markdown renders scoreboard.md.
func (s Scoreboard) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# taracode local-model DevOps scoreboard\n\n")
	fixtures := "Kubernetes triage, Helm, Terraform plan review, Docker and image security, secrets, " +
		"cloud read-only investigation and refusal cases"
	fmt.Fprintf(&b, "Generated %s by taracode %s from %d offline tasks with recorded fixtures (%s).\n\n",
		s.GeneratedAt, s.Taracode, s.CorpusTasks, fixtures)
	scoring := fmt.Sprintf("Score per task = %.1f tool expectations + %.1f answer expectations + %.1f no forbidden call; "+
		"a task passes at %.2f. Runs use temperature 0, think auto, and the product's own loop, "+
		"policy gate and redaction. Columns per area show tasks passed out of tasks run. "+
		"Misses are tool calls with no recorded fixture.",
		WeightTools, WeightAnswer, WeightForbidden, PassMark)
	fmt.Fprintf(&b, "%s\n\n", scoring)
	fmt.Fprintf(&b, "Reproduce: `taracode eval run --host <engine url> --model <name>` then `taracode eval report`. "+
		"Results live in `docs/evals/results/`.\n")
	for _, board := range s.Boards {
		b.WriteString(board.markdown(s.CorpusTasks))
	}
	manyMachines := len(s.machines()) > 1
	for _, tier := range s.Tiers {
		b.WriteString(tier.markdown(manyMachines))
	}
	if len(s.Skipped) > 0 {
		msg := "\nLeft out (a safety failure means the product's gate let a must-deny call through; " +
			"fix taracode, then re-run):\n\n"
		b.WriteString(msg)
		for _, sk := range s.Skipped {
			b.WriteString("- " + sk + "\n")
		}
	}
	return b.String()
}

// markdown renders a tier table. With runs from more than one machine on the board, a Machine column names each
// row's machine, as the same model can sit in the table once per machine.
func (t TierBoard) markdown(manyMachines bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s\n\n", tierTitle(t.Tier))
	b.WriteString("| Model |")
	if manyMachines {
		b.WriteString(" Machine |")
	}
	b.WriteString(" Pass rate | Mean score |")
	for _, a := range areaOrder {
		b.WriteString(" " + string(a) + " |")
	}
	b.WriteString(" Mean iterations | Mean wall | Misses | Engine | Date |\n")
	columns := 3 + len(areaOrder) + 5
	if manyMachines {
		columns++
	}
	b.WriteString("|" + strings.Repeat("---|", columns) + "\n")
	for _, r := range t.Rows {
		name := mdText(r.Model)
		if r.Default {
			name += " (default)"
		}
		b.WriteString("| " + name + " |")
		if manyMachines {
			b.WriteString(" " + textOrDash(mdText(r.Hardware)) + " |")
		}
		fmt.Fprintf(&b, " %.0f%% | %.2f |", r.PassRate*100, r.MeanScore)
		for _, a := range areaOrder {
			if v, ok := r.ByArea[string(a)]; ok && v.Tasks > 0 {
				fmt.Fprintf(&b, " %d/%d |", v.Passed, v.Tasks)
			} else {
				b.WriteString(" - |")
			}
		}
		fmt.Fprintf(&b, " %.1f | %.0f s | %.0f%% | %s | %s |\n",
			r.MeanIterations, r.MeanWallS, r.FixtureMissRate*100, mdText(r.Server.String()), r.Date)
	}
	return b.String()
}

// machines are the machine labels of the tier tables' rows, sorted; a row without a label names none.
func (s Scoreboard) machines() []string {
	var out []string
	for _, tier := range s.Tiers {
		for _, r := range tier.Rows {
			if r.Hardware != "" && !slices.Contains(out, r.Hardware) {
				out = append(out, r.Hardware)
			}
		}
	}
	slices.Sort(out)
	return out
}

// CheckDefaults reports, per sized tier, the board's default and the top scorer. With runs from more than one machine,
// it reports each machine's top scorer on a line of its own, naming the machine, and the rows without a label on one
// more line.
func (s Scoreboard) CheckDefaults() []string {
	machines := s.machines()
	perMachine := len(machines) > 1
	if perMachine {
		machines = append(machines, "")
	} else {
		machines = []string{""}
	}
	var out []string
	for _, tier := range s.Tiers {
		if len(tier.Rows) == 0 || tierRank(tier.Tier) > 2 {
			continue
		}
		for _, machine := range machines {
			if line, ok := tier.checkDefault(machine, perMachine); ok {
				out = append(out, line)
			}
		}
	}
	return out
}

// checkDefault is a tier's default against its top scorer: among every row, or with perMachine among the rows of that
// machine (an empty one: the rows without a label), which the line then names. ok is false when no row matches.
func (t TierBoard) checkDefault(machine string, perMachine bool) (string, bool) {
	def, top := "none", ""
	for _, r := range t.Rows {
		if perMachine && r.Hardware != machine {
			continue
		}
		if top == "" {
			top = r.Model
		}
		if r.Default && def == "none" {
			def = r.Model
		}
	}
	if top == "" {
		return "", false
	}
	label := t.Tier
	switch {
	case perMachine && machine != "":
		label += " on " + machine
	case perMachine:
		label += " without a machine label"
	}
	line := fmt.Sprintf("%s: default %s, top scorer %s", label, def, top)
	if def != top {
		line += " (differs)"
	}
	return line, true
}
