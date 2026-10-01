package evals

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/models"
)

func TestWriteResultsReportsFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "results")
	if err := os.WriteFile(file, []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteResults(file, Results{Model: "gemma4:12b", Date: "2026-09-26"}); err == nil {
		t.Fatal("a results directory that is a file")
	}
	if _, err := WriteResults(t.TempDir(), Results{Model: "gemma4:12b", Temperature: math.NaN()}); err == nil {
		t.Fatal("results JSON cannot carry")
	}
}

func TestReadResultsReportsFailures(t *testing.T) {
	if _, err := ReadResults(filepath.Join(t.TempDir(), "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing directory: %v", err)
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResults(broken); err == nil || !strings.HasPrefix(err.Error(), "broken.json: ") {
		t.Fatalf("broken file: %v", err)
	}
	dangling := t.TempDir()
	if err := os.Symlink(filepath.Join(dangling, "gone"), filepath.Join(dangling, "dangling.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResults(dangling); !os.IsNotExist(err) {
		t.Fatalf("a file that cannot be read: %v", err)
	}
}

// TestReadResultsSortsByModelThenDate: the order is the model name's and then the date's, not the
// file names' (the slug lowercases the model).
func TestReadResultsSortsByModelThenDate(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []Results{{Model: "a:1b", Date: "2026-09-02"}, {Model: "B:1b", Date: "2026-09-03"},
		{Model: "B:1b", Date: "2026-09-01"}} {
		if _, err := WriteResults(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadResults(dir)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	var order []string
	for _, r := range got {
		order = append(order, r.Model+"@"+r.Date)
	}
	if strings.Join(order, " ") != "B:1b@2026-09-01 B:1b@2026-09-03 a:1b@2026-09-02" {
		t.Fatalf("%v", order)
	}
}

// TestSummarizeWeighsAnUnweightedTaskAsOne: a task with no weight, or a result for a task the list
// lacks, counts with weight 1.
func TestSummarizeWeighsAnUnweightedTaskAsOne(t *testing.T) {
	s := summarize([]Task{{ID: "a", Area: AreaKubernetes}}, []TaskResult{{ID: "a", Area: "kubernetes", Score: 0.5},
		{ID: "b", Area: "kubernetes", Score: 1}})
	if s.MeanScore != 0.75 {
		t.Fatalf("mean score %v", s.MeanScore)
	}
}

func TestPublicErrorOfNoError(t *testing.T) {
	if got := publicError(nil, RunOptions{}, Task{}); got != "" {
		t.Fatalf("%q", got)
	}
}

func hasPair(pairs [][2]string, with string) bool {
	for _, p := range pairs {
		if p[1] == with {
			return true
		}
	}
	return false
}

// TestScrubbedPathsWithoutARunScope: outside a run the home directory comes from the environment;
// an unknown home, or a home at the root, is not scrubbed.
func TestScrubbedPathsWithoutARunScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pairs := scrubbedPaths(RunOptions{}, Task{})
	found := false
	for _, p := range pairs {
		found = found || (p[0] == home && p[1] == "~")
	}
	if !found {
		t.Fatalf("home is scrubbed: %v", pairs)
	}
	for _, unusable := range []string{"", "/"} {
		t.Setenv("HOME", unusable)
		if hasPair(scrubbedPaths(RunOptions{}, Task{}), "~") {
			t.Errorf("HOME=%q is not scrubbed", unusable)
		}
	}
}

func TestEngineHostForms(t *testing.T) {
	if engineForms("  ") != nil || engineHostname("") != "" || engineHostname("http://%zz") != "" {
		t.Fatal("an empty or unparsable host has no forms")
	}
	if u := engineURL("gpu-box:11434"); u == nil || u.Scheme != "http" || u.Host != "gpu-box:11434" {
		t.Fatalf("a bare host:port is read as http: %v", u)
	}
	if engineURL("http://") != nil {
		t.Fatal("a URL without a host")
	}
	if got := engineHostname(" gpu-box:11434 "); got != "gpu-box" {
		t.Fatalf("%q", got)
	}
}

func TestScoreboardPutsAnUnknownModelInOther(t *testing.T) {
	reg, err := models.Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := BuildScoreboard([]Results{{Model: "mystery:1b", Date: "2026-09-01"}}, reg, 1, "test")
	if len(sb.Tiers) != 1 || sb.Tiers[0].Tier != "other" || sb.Tiers[0].Rows[0].Model != "mystery:1b" {
		t.Fatalf("%+v", sb.Tiers)
	}
	bad := Scoreboard{Tiers: []TierBoard{{Tier: "16", Rows: []Row{{Model: "m", PassRate: math.NaN()}}}}}
	if _, err := bad.JSON(); err == nil {
		t.Fatal("a scoreboard JSON cannot carry")
	}
}
