package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTaskNeedsATaskFile(t *testing.T) {
	if _, err := LoadTask(t.TempDir()); !os.IsNotExist(err) {
		t.Fatalf("err = %v", err)
	}
}

// TestValidateNamesEachProblem: each broken field of an otherwise valid task is named in the error.
func TestValidateNamesEachProblem(t *testing.T) {
	base, err := LoadTask(writeTask(t, t.TempDir(), "crashloop-oomkilled", goodTask))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Task)
		want   string
	}{
		{"id", func(t *Task) { t.ID, t.Dir = "Bad_ID", "" }, `id "Bad_ID" must match`},
		{"provenance", func(t *Task) { t.Provenance = "guessed" }, `provenance "guessed" must be recorded, authored or files`},
		{"prompt", func(t *Task) { t.Prompt = "  " }, "prompt is empty"},
		{"weight", func(t *Task) { t.Weight = -1 }, "weight must be positive"},
		{"permission", func(t *Task) { t.Permission = "maybe" }, `permission "maybe" must be allow or deny`},
		{"record tool", func(t *Task) { t.Record.Calls = []RecordCall{{Tool: "kubectl2"}} }, `record.calls[0]: unknown tool "kubectl2"`},
		{"empty matcher", func(t *Task) { t.Expect.ToolsNever = []Matcher{{}} }, "tools_never[0]: empty matcher"},
		{"classification", func(t *Task) { t.Expect.ToolsNever = []Matcher{{Classification: "write"}} },
			`classification "write" must be read or mutate`},
		{"signature regexp", func(t *Task) { t.Expect.ToolsNever = []Matcher{{SignatureMatches: "("}} }, `signature_matches "(": `},
	}
	for _, tt := range tests {
		task := base
		record := *base.Record
		task.Record = &record
		tt.mutate(&task)
		if err := task.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestLoadCorpusReportsWhatItCannotLoad(t *testing.T) {
	if _, err := LoadCorpus(filepath.Join(t.TempDir(), "missing"), ""); !os.IsNotExist(err) {
		t.Fatalf("missing root: %v", err)
	}
	root := t.TempDir()
	writeTask(t, root, "crashloop-oomkilled", goodTask)
	writeTask(t, root, "broken-task", "id: broken-task\n")
	if _, err := LoadCorpus(root, ""); err == nil || !strings.Contains(err.Error(), "broken-task") {
		t.Fatalf("a broken task: %v", err)
	}
	if tasks, err := LoadCorpus(root, "crash*"); err != nil || len(tasks) != 1 {
		t.Fatalf("the glob leaves the broken task out: %v %v", tasks, err)
	}
}
