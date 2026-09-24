package evals

import (
	"path/filepath"
	"testing"
)

func TestWriteAndReadResults(t *testing.T) {
	dir := t.TempDir()
	r := Results{Taracode: "3.0.0-beta.1", Model: "qwen3.8:27b", Tier: "32", Date: "2026-09-26",
		Tasks:   []TaskResult{{ID: "a", Area: "kubernetes", Score: 0.9, Pass: true}},
		Summary: Summary{PassRate: 1, MeanScore: 0.9, ByArea: map[string]AreaSummary{"kubernetes": {Tasks: 1, Passed: 1, MeanScore: 0.9}}}}
	path, err := WriteResults(dir, r)
	if err != nil || filepath.Base(path) != "qwen3.8-27b-2026-09-26.json" {
		t.Fatalf("%q %v", path, err)
	}
	all, err := ReadResults(dir)
	if err != nil || len(all) != 1 || all[0].Model != "qwen3.8:27b" || all[0].Tasks[0].Score != 0.9 {
		t.Fatalf("%+v %v", all, err)
	}
	if modelSlug("hf.co/org/model:Q4") != "hf.co-org-model-q4" {
		t.Fatal(modelSlug("hf.co/org/model:Q4"))
	}
}

func TestSummarizeWeightsTheScoreAndCountsAreas(t *testing.T) {
	tasks := []Task{{ID: "a", Weight: 3}, {ID: "b", Weight: 1}}
	s := summarize(tasks, []TaskResult{
		{ID: "a", Area: "kubernetes", Score: 1, Pass: true, Iterations: 4, WallMs: 100, ToolCalls: 3, FixtureMisses: 1},
		{ID: "b", Area: "helm", Score: 0.2, Iterations: 2, WallMs: 300, ToolCalls: 1, SafetyFailure: true},
	})
	if s.MeanScore != 0.8 || s.PassRate != 0.5 || s.MeanIterations != 3 || s.MeanWallMs != 200 ||
		s.FixtureMissRate != 0.25 || s.SafetyFailures != 1 {
		t.Fatalf("%+v", s)
	}
	if a := s.ByArea["kubernetes"]; a.Tasks != 1 || a.Passed != 1 || a.MeanScore != 1 {
		t.Errorf("kubernetes %+v", a)
	}
	if a := s.ByArea["helm"]; a.Tasks != 1 || a.Passed != 0 || a.MeanScore != 0.2 {
		t.Errorf("helm %+v", a)
	}
	if empty := summarize(nil, nil); empty.ByArea == nil || empty.PassRate != 0 || empty.MeanScore != 0 {
		t.Errorf("empty %+v", empty)
	}
}
