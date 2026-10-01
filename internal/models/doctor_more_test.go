package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	ollamaclient "github.com/tara-vision/taracode/internal/llm/ollama"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// TestDiagnoseNotesAnInstalledRecommendationOnAnOldServer: with the tier's default installed, the
// advice says so, and a server older than the default needs gets the version note.
func TestDiagnoseNotesAnInstalledRecommendationOnAnOldServer(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	rec := reg.DefaultForTier(Tier48)
	if rec.MinOllama == "" {
		t.Fatalf("the 48 GB default needs a minimum Ollama for this test: %+v", rec)
	}
	srv := ollamatest.New(t)
	srv.Version = "0.1.0"
	srv.Models = []ollamatest.ModelSpec{{Name: rec.Name, Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	rep := Diagnose(context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 64, "", noLookup, nil)
	if !rep.RecommendationInstalled {
		t.Fatalf("%+v", rep)
	}
	text := rep.Render()
	for _, want := range []string{
		"Advice    " + rec.Name + " is installed and is the recommended model for this machine\n",
		"          needs Ollama >= " + rec.MinOllama + " (server has 0.1.0)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("render lacks %q:\n%s", want, text)
		}
	}
}

// TestDiagnoseCallsAServerUnreachableWhenItCannotListModels: a server that answers the version but
// not the model list is not reported as reachable.
func TestDiagnoseCallsAServerUnreachableWhenItCannotListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = w.Write([]byte(`{"version":"0.34.2"}`))
			return
		}
		http.Error(w, `{"error":"tags failed"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	rep := Diagnose(context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 16, "", noLookup, nil)
	if rep.ServerOK || !strings.Contains(rep.ServerError, "tags failed") || len(rep.Models) != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestVersionsThatDoNotParse(t *testing.T) {
	if versionBefore("0.5.0", "not.a.version") {
		t.Fatal("an unparsable requirement is never newer")
	}
	if _, ok := parseVersion("1.2.3.4"); ok {
		t.Fatal("four components")
	}
}

// fakeSysctl puts a sysctl script first on PATH.
func fakeSysctl(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("HostRAMGB asks sysctl on macOS only")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sysctl"), []byte(script), 0o755); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestHostRAMGBReportsSysctlFailures(t *testing.T) {
	fakeSysctl(t, "#!/bin/sh\nexit 1\n")
	if _, err := HostRAMGB(); err == nil || !strings.HasPrefix(err.Error(), "models: sysctl hw.memsize: ") {
		t.Fatalf("a failing sysctl: %v", err)
	}
	fakeSysctl(t, "#!/bin/sh\necho lots\n")
	if _, err := HostRAMGB(); err == nil || !strings.HasPrefix(err.Error(), "models: parse hw.memsize: ") {
		t.Fatalf("an unreadable answer: %v", err)
	}
	fakeSysctl(t, "#!/bin/sh\necho 68719476736\n")
	if gb, err := HostRAMGB(); err != nil || gb != 64 {
		t.Fatalf("64 GiB: %d %v", gb, err)
	}
}

// TestRecommendationNeedsAnEntryForTheTier: a registry without an entry for the host's tier
// recommends nothing.
func TestRecommendationNeedsAnEntryForTheTier(t *testing.T) {
	rep := &Report{Tier: Tier48, registry: &Registry{Entries: []Entry{{Name: "small-model", Tier: Tier16, Default: true}}}}
	if e, ok := rep.Recommendation(); ok || e.Name != "" {
		t.Fatalf("%+v %v", e, ok)
	}
}
