package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func TestRunServesTheCommonStoreAndMarksTheCall(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	common := filepath.Join(t.TempDir(), "common")
	store, err := LoadFixtures(common)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("shell which kubectl", "/usr/local/bin/kubectl", false); err != nil {
		t.Fatal(err)
	}
	srv := fakeEngine(t,
		call("shell", map[string]any{"command": "which kubectl"}),
		call("kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}),
		call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"}),
		call("shell", map[string]any{"command": "kubectl logs checkout-1 -n shop --previous"}),
		taraenginetest.Turn{Content: "The container was OOMKilled: it exceeded its memory limit of 64Mi."},
	)
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.CommonDir = common
	res, err := Run(context.Background(), tasks, opts)
	if err != nil || len(res.Tasks) != 1 || !res.Tasks[0].Pass || res.Tasks[0].FixtureMisses != 0 {
		t.Fatalf("results %+v err=%v", res.Tasks, err)
	}
	data, err := os.ReadFile(filepath.Join(runsDir, "gemma4-12b-2026-09-26", "crashloop-oomkilled.log"))
	if err != nil || !strings.Contains(string(data), "COMMON shell which kubectl") {
		t.Fatalf("transcript %v:\n%s", err, data)
	}
	if err := os.WriteFile(filepath.Join(common, "fixtures", "index.yaml"), []byte("fixtures: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), tasks, opts); err == nil || !strings.HasPrefix(err.Error(), "the common fixtures: ") {
		t.Fatalf("a common store that cannot be read: %v", err)
	}
}
