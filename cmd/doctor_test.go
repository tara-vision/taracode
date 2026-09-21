package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/models"
)

// TestRunDoctorAgainstAClosedPortIsUnreachable covers `taracode doctor` against a server that
// cannot be reached: runDoctor is the testable core RunE wraps (asserting a real process exit
// code is awkward under go test, per the brief), so this drives it directly with the same closed
// port the Step 5 smoke test uses, and checks both the rendered "unreachable" text and the exit
// code path doctorExitCode feeds into os.Exit.
func TestRunDoctorAgainstAClosedPortIsUnreachable(t *testing.T) {
	rep, err := runDoctor(context.Background(), "http://127.0.0.1:1", "", "", "")
	if err != nil {
		t.Fatalf("runDoctor() error = %v, want a report even when the server is unreachable", err)
	}
	if rep.ServerOK {
		t.Fatalf("expected an unreachable report: %+v", rep)
	}
	if !strings.Contains(rep.Render(), "unreachable") {
		t.Fatalf("render lacks \"unreachable\": %s", rep.Render())
	}
	if code := doctorExitCode(rep); code != 1 {
		t.Fatalf("doctorExitCode() = %d, want 1 for an unreachable server", code)
	}
}

// TestRunDoctorUsesTheModelFlagVariable proves the configuredModel RunE threads into Diagnose
// comes from the --model flag variable (model, from root.go's StringVar), not
// viper.GetString("model") - "model" is a config section (model.temperature, model.top_p, ...)
// in config.yaml, so as a plain viper string key it is always "". Render prints the Model line
// unconditionally whenever ConfiguredModel != "", so this holds even against the closed-port
// server RunE would otherwise consider unreachable.
func TestRunDoctorUsesTheModelFlagVariable(t *testing.T) {
	model = "x:1b"
	defer func() { model = "" }()

	rep, err := runDoctor(context.Background(), "http://127.0.0.1:1", "", "", model)
	if err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
	if !strings.Contains(rep.Render(), "Model     x:1b") {
		t.Fatalf("render lacks the configured model from the --model flag:\n%s", rep.Render())
	}
}

// TestDoctorExitCode covers both sides of the RunE -> os.Exit condition.
func TestDoctorExitCode(t *testing.T) {
	if got := doctorExitCode(models.Report{ServerOK: true}); got != 0 {
		t.Fatalf("doctorExitCode(reachable) = %d, want 0", got)
	}
	if got := doctorExitCode(models.Report{ServerOK: false}); got != 1 {
		t.Fatalf("doctorExitCode(unreachable) = %d, want 1", got)
	}
}

// TestResolveDoctorTarget covers the host resolution doctor shares with the REPL: --host (bound
// to viper) wins outright, and otherwise the hosts: config's default host fills in the host, API
// key and vendor.
func TestResolveDoctorTarget(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	viper.Set("host", "http://flag:1")
	viper.Set("hosts", map[string]any{
		"primary": map[string]any{"url": "http://primary:2", "api_key": "k", "vendor": "ollama"},
	})
	viper.Set("default_host", "primary")

	if h, _, _ := resolveDoctorTarget(); h != "http://flag:1" {
		t.Fatalf("host = %q, want the --host flag to win over the hosts: default", h)
	}

	viper.Reset()
	viper.Set("hosts", map[string]any{
		"primary": map[string]any{"url": "http://primary:2", "api_key": "k", "vendor": "ollama"},
	})
	viper.Set("default_host", "primary")

	h, apiKey, vendor := resolveDoctorTarget()
	if h != "http://primary:2" || apiKey != "k" || vendor != "ollama" {
		t.Fatalf("resolveDoctorTarget() = (%q, %q, %q), want the default host's values", h, apiKey, vendor)
	}
}

// TestHandleDoctorRunsAgainstTheLiveAssistant covers the REPL's /doctor: it must run the same
// diagnosis against the assistant's already-connected provider, model and host, with no extra
// network setup of its own.
func TestHandleDoctorRunsAgainstTheLiveAssistant(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t)
	srv.Version = "0.34.2"
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}

	a, err := assistant.New(srv.URL, "", "gemma4:12b", "ollama", false, false)
	if err != nil {
		t.Fatalf("assistant.New() = %v", err)
	}

	out := captureStdoutForTest(t, func() { handleDoctor(a) })

	for _, want := range []string{"Ollama 0.34.2", "gemma4:12b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("handleDoctor output lacks %q:\n%s", want, out)
		}
	}
}

// captureStdoutForTest runs fn with os.Stdout replaced by a pipe and returns everything it wrote.
func captureStdoutForTest(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()

	fn()

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}
