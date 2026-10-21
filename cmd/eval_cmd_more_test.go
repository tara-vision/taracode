package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/tara-vision/taracode/internal/evals"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// evalsRoot is a corpus as the repository lays it out: <evals>/tasks with one task, for the seal and the run.
func evalsRoot(t *testing.T) (evalsDir, corpus string) {
	t.Helper()
	evalsDir = t.TempDir()
	corpus = filepath.Join(evalsDir, "tasks")
	dir := filepath.Join(corpus, "partial-defect")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte(partialDefectTask), 0o644); err != nil {
		t.Fatal(err)
	}
	return evalsDir, corpus
}

// resetEvalFlags clears the flags these tests pass to the real root command, which cobra keeps between runs.
func resetEvalFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, f := range []*pflag.Flag{evalLintCmd.Flags().Lookup("seal"), evalLintCmd.Flags().Lookup("version"),
			evalLintCmd.Flags().Lookup("results"), evalRecordCmd.Flags().Lookup("common")} {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	})
}

// execute runs the root command with args, a throwaway HOME and config, and resets the flags after.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetConfig(t)
	isolateHome(t)
	resetEvalFlags(t)
	return executeWith(t, args...)
}

func TestEvalLintSealsTheCorpus(t *testing.T) {
	evalsDir, corpus := evalsRoot(t)
	out, err := execute(t, "eval", "lint", "--corpus", corpus, "--seal", "--version", "2")
	if err != nil || !strings.Contains(out, "sealed: corpus version 2, hash ") {
		t.Fatalf("%v %q", err, out)
	}
	seal, err := os.ReadFile(filepath.Join(evalsDir, "VERSION"))
	if err != nil || !strings.HasPrefix(string(seal), "version: 2\nhash: ") {
		t.Fatalf("%v %q", err, seal)
	}
	if err := runEvalSeal(corpus, "", io.Discard); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatalf("no version: %v", err)
	}
	if err := runEvalSeal(filepath.Join(evalsDir, "none", "tasks"), "3", io.Discard); err == nil {
		t.Fatal("a seal nowhere to write")
	}
}

func TestEvalRecordCommonRecordsTheStore(t *testing.T) {
	dir := t.TempDir()
	calls := "calls:\n  - {tool: shell, args: {command: \"echo hello\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "calls.yaml"), []byte(calls), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execute(t, "eval", "record", "--common", dir); err != nil {
		t.Fatalf("%v %q", err, out)
	}
	store, err := evals.LoadFixtures(dir)
	if err != nil || store.Len() != 1 {
		t.Fatalf("store %v %v", store, err)
	}
	if text, isErr, ok := store.Lookup("shell echo hello"); !ok || isErr || text != "hello" {
		t.Fatalf("%q %v %v", text, isErr, ok)
	}
}

func TestEvalRunRefusesACorpusThatChangedUnderItsSeal(t *testing.T) {
	evalsDir, corpus := evalsRoot(t)
	seal := filepath.Join(evalsDir, "VERSION")
	if err := os.WriteFile(seal, []byte("version: 3\nhash: "+strings.Repeat("0", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := evalRunFlags{host: "http://127.0.0.1:1", model: "gemma4:12b", corpus: corpus, think: "auto", runs: 1,
		timeout: time.Second, out: t.TempDir()}
	err := runEvalRun(f, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "changed since it was sealed as version 3") {
		t.Fatalf("%v", err)
	}
	if err := os.Remove(seal); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(seal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runEvalRun(f, io.Discard); err == nil || !strings.Contains(err.Error(), "VERSION") {
		t.Fatalf("a seal that cannot be read: %v", err)
	}
}

func TestEvalReportRefusesTwoCorpusVersions(t *testing.T) {
	results, out := t.TempDir(), t.TempDir()
	for i, model := range []string{"gemma4:12b", "qwen3.6:35b"} {
		r := evals.Results{Taracode: "t", Model: model, Tier: "16", Date: "2026-09-26",
			Corpus:  &evals.Corpus{Version: string(rune('1' + i))},
			Summary: evals.Summary{PassRate: 1, MeanScore: 1, ByArea: map[string]evals.AreaSummary{}}}
		if _, err := evals.WriteResults(results, r); err != nil {
			t.Fatal(err)
		}
	}
	err := runEvalReport(results, out, t.TempDir(), false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "2 corpus versions") {
		t.Fatalf("%v", err)
	}
}

func TestEvalRunRefusesAnOverridePathOutsideTheOverridesFolder(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, p := range []string{"../x.registry.yaml", "docs/evals/x.registry.yaml", "docs/evals/overrides/../x.registry.yaml",
		"docs/evals/overrides/x.yaml"} {
		err := runEvalRun(evalRunFlags{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "auto", override: p}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "--override") {
			t.Fatalf("%q: %v", p, err)
		}
	}
}

func TestEvalRunRefusesAnOverrideThatDoesNotExist(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runEvalRun(evalRunFlags{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "auto",
		override: "docs/evals/overrides/x.registry.yaml"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--override") || !strings.Contains(err.Error(), "x.registry.yaml") {
		t.Fatalf("%v", err)
	}
}

func TestEvalRunRecordsTheOverrideAndWarnsWithoutOne(t *testing.T) {
	t.Chdir(t.TempDir())
	override := "docs/evals/overrides/nemotron-3.5-bolt-74b.registry.yaml"
	if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("models: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, corpus := evalsRoot(t)
	srv := fakeEvalEngine(t, taraenginetest.Turn{Content: "done"})
	srv.Models[0].Origin = "registry"
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_300_000_000}}
	out := t.TempDir()
	f := evalRunFlags{host: srv.URL, model: "gemma4:12b", corpus: corpus, think: "auto", runs: 1,
		timeout: 30 * time.Second, out: out, hostLabel: "lab", hardware: "NVIDIA RTX 5090 (32 GB)", override: override}
	if err := runEvalRun(f, io.Discard); err != nil {
		t.Fatal(err)
	}
	all, err := evals.ReadResults(out)
	if err != nil || len(all) != 1 || all[0].Override != override || all[0].Engine == nil || all[0].Engine.Origin != "registry" {
		t.Fatalf("results %+v err=%v", all, err)
	}
	srv.Turns = append(srv.Turns, taraenginetest.Turn{Content: "done"})
	f.override, f.out = "", t.TempDir()
	var warned bytes.Buffer
	if err := runEvalRun(f, &warned); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warned.String(), "warning: the engine serves gemma4:12b from a registry file and no --override names "+
		"its published copy: the row will not rank") {
		t.Fatalf("out %q", warned.String())
	}
}

func TestEvalLintChecksTheResultsOverrides(t *testing.T) {
	t.Chdir(t.TempDir())
	corpus := t.TempDir() // an empty corpus lints clean, so the one problem is the results'
	results := t.TempDir()
	r := evals.Results{Taracode: "t", Model: "nemotron-3.5-bolt:74b", Tier: "48", Date: "2026-10-20",
		Override: "docs/evals/overrides/none.registry.yaml", Summary: evals.Summary{ByArea: map[string]evals.AreaSummary{}}}
	if _, err := evals.WriteResults(results, r); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runEvalLint(corpus, results, &out)
	if err == nil || !strings.Contains(err.Error(), "1 problem") {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if err := runEvalLint(corpus, "", io.Discard); err != nil {
		t.Fatalf("without --results: %v", err)
	}
}
