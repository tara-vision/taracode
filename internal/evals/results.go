package evals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Results is one model's run over the corpus (spec 9).
type Results struct {
	Taracode    string       `json:"taracode"`
	Ollama      string       `json:"ollama"`
	Model       string       `json:"model"`
	Tier        string       `json:"tier"`
	Think       string       `json:"think"`
	Temperature float64      `json:"temperature"`
	Date        string       `json:"date"`
	Runs        int          `json:"runs"`
	Host        string       `json:"host"`
	Tasks       []TaskResult `json:"tasks"`
	Summary     Summary      `json:"summary"`
}

// TaskResult is one task's row.
type TaskResult struct {
	ID               string   `json:"id"`
	Area             string   `json:"area"`
	Score            float64  `json:"score"`
	Pass             bool     `json:"pass"`
	Tools            float64  `json:"tools"`
	Answer           float64  `json:"answer"`
	Forbidden        float64  `json:"forbidden"`
	Iterations       int      `json:"iterations"`
	ToolCalls        int      `json:"tool_calls"`
	Denied           int      `json:"denied"`
	FixtureMisses    int      `json:"fixture_misses"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	WallMs           int64    `json:"wall_ms"`
	Truncated        bool     `json:"truncated"`
	TimedOut         bool     `json:"timed_out"`
	SafetyFailure    bool     `json:"safety_failure"`
	Error            string   `json:"error"`
	Notes            []string `json:"notes,omitempty"`
}

// AreaSummary is the per-area breakdown.
type AreaSummary struct {
	Tasks     int     `json:"tasks"`
	Passed    int     `json:"passed"`
	MeanScore float64 `json:"mean_score"`
}

// Summary is the run's totals.
type Summary struct {
	PassRate        float64                `json:"pass_rate"`
	MeanScore       float64                `json:"mean_score"`
	MeanIterations  float64                `json:"mean_iterations"`
	MeanWallMs      float64                `json:"mean_wall_ms"`
	FixtureMissRate float64                `json:"fixture_miss_rate"`
	SafetyFailures  int                    `json:"safety_failures"`
	ByArea          map[string]AreaSummary `json:"by_area"`
}

// modelSlug makes a model name safe for a file name.
func modelSlug(model string) string {
	return strings.ToLower(strings.NewReplacer(":", "-", "/", "-").Replace(model))
}

// WriteResults writes <dir>/<model slug>-<date>.json and returns its path.
func WriteResults(dir string, r Results) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // docs/evals/results is repository content
		return "", err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, modelSlug(r.Model)+"-"+r.Date+".json")
	return path, os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // repository content
}

// ReadResults reads every *.json under dir, sorted by file name.
func ReadResults(dir string) ([]Results, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Results
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // a results file the caller points at
		if err != nil {
			return nil, err
		}
		var r Results
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model+out[i].Date < out[j].Model+out[j].Date })
	return out, nil
}

// summarize computes the totals of a run from its task results and the tasks' weights.
func summarize(tasks []Task, results []TaskResult) Summary {
	s := Summary{ByArea: map[string]AreaSummary{}}
	if len(results) == 0 {
		return s
	}
	weights := map[string]float64{}
	for _, t := range tasks {
		weights[t.ID] = t.Weight
	}
	var passed int
	var weightSum, scoreSum, iterations, wall float64
	var calls, misses int
	areaScores := map[string][]float64{}
	for _, r := range results {
		w := weights[r.ID]
		if w == 0 {
			w = 1
		}
		weightSum += w
		scoreSum += w * r.Score
		iterations += float64(r.Iterations)
		wall += float64(r.WallMs)
		calls += r.ToolCalls
		misses += r.FixtureMisses
		if r.Pass {
			passed++
		}
		if r.SafetyFailure {
			s.SafetyFailures++
		}
		a := s.ByArea[r.Area]
		a.Tasks++
		if r.Pass {
			a.Passed++
		}
		s.ByArea[r.Area] = a
		areaScores[r.Area] = append(areaScores[r.Area], r.Score)
	}
	n := float64(len(results))
	s.PassRate = round3(float64(passed) / n)
	s.MeanScore = round3(scoreSum / weightSum)
	s.MeanIterations = round3(iterations / n)
	s.MeanWallMs = round3(wall / n)
	if calls > 0 {
		s.FixtureMissRate = round3(float64(misses) / float64(calls))
	}
	for area, scores := range areaScores {
		sum := 0.0
		for _, v := range scores {
			sum += v
		}
		a := s.ByArea[area]
		a.MeanScore = round3(sum / float64(len(scores)))
		s.ByArea[area] = a
	}
	return s
}
