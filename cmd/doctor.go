package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/models"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/provider"
)

// doctorCmd is `taracode doctor`: it checks the LLM server, the installed models and their
// capabilities, the host RAM tier and the registry's recommendation for it, and the external
// tools on PATH, then prints the report and exits 1 when the server could not be reached.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the LLM server, installed models and external tools",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		targetHost, apiKey, vendor, configuredModel := resolveDoctorTarget()
		if targetHost == "" {
			return fmt.Errorf("LLM server host not found; set --host, TARACODE_HOST, or host: in config.yaml")
		}
		rep, err := runDoctor(cmd.Context(), targetHost, apiKey, vendor, configuredModel)
		if err != nil {
			return err
		}
		if cwd, err := os.Getwd(); err == nil {
			rep.PolicyNote = policyNote(cwd)
		}
		fmt.Print(rep.Render())
		if code := doctorExitCode(rep); code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

// resolveDoctorTarget picks the host, API key, vendor and model to check: --host and --model
// (bound to viper), TARACODE_HOST, or config.yaml's top-level host:, key:, vendor: and model:
// keys. Same resolution newREPL runs at the top of the REPL. "model" is a plain viper string key
// in v3 (--model is bound to it; the 2.x model: section is a map, which viper.GetString turns
// into "").
func resolveDoctorTarget() (targetHost, apiKey, vendor, configuredModel string) {
	return viper.GetString("host"), viper.GetString("key"), viper.GetString("vendor"), viper.GetString("model")
}

// doctorResolveWindow is the resolveWindow function every Diagnose call in this file passes: the
// context window taracode would request for a model with this native maximum, from the same
// config key and resolver a live turn uses.
func doctorResolveWindow(modelMax int) (int, string) {
	return agent.ResolveContextWindow(viper.GetString("context.window"), modelMax)
}

// runDoctor builds a provider for hostURL and runs models.Diagnose against it. It is the testable
// core of the command: asserting a real process exit code is awkward under go test, so tests call
// this (and doctorExitCode) directly instead of driving the command through cobra's Execute.
func runDoctor(ctx context.Context, hostURL, apiKey, vendor, configuredModel string) (models.Report, error) {
	prov, err := provider.New(ctx, hostURL, vendor, apiKey)
	if err != nil {
		return models.Report{}, fmt.Errorf("doctor: create provider: %w", err)
	}
	ramGB, _ := models.HostRAMGB()
	return models.Diagnose(ctx, prov.LLM(), hostURL, ramGB, configuredModel, exec.LookPath, doctorResolveWindow), nil
}

// doctorExitCode is 1 when the server could not be reached, the condition RunE turns into
// os.Exit(1).
func doctorExitCode(rep models.Report) int {
	if rep.ServerOK {
		return 0
	}
	return 1
}

// policyNote is the doctor's policy line: which files load, or the parse error.
func policyNote(projectDir string) string {
	home, _ := os.UserHomeDir()
	_, sources, err := policy.Load(projectDir, home)
	if err != nil {
		return "error: " + err.Error()
	}
	return "ok (" + strings.Join(sources, ", ") + ")"
}

// cmdDoctor is the /doctor command: diagnose the LLM server and tools.
func (r *repl) cmdDoctor(_ []string) {
	handleDoctor(r.asst, r.projectRoot)
}

// handleDoctor runs the doctor's diagnosis against the assistant's live connection: the REPL's
// /doctor, mirroring `taracode doctor` without leaving the session. projectRoot supplies the policy
// line: it is the sandbox root (r.projectRoot), not whatever the assistant's own working directory
// happens to be, since the assistant is re-created on /init, /model and /reload while the project
// root never moves during a session.
func handleDoctor(asst *agent.Assistant, projectRoot string) {
	liveHost := ""
	if info := asst.GetProviderInfo(); info != nil {
		liveHost = info.Host
	}
	ramGB, _ := models.HostRAMGB()
	rep := models.Diagnose(
		context.Background(), asst.GetProvider().LLM(), liveHost, ramGB, asst.GetCurrentModel(), exec.LookPath,
		doctorResolveWindow,
	)
	rep.PolicyNote = policyNote(projectRoot)
	fmt.Print(rep.Render())
	fmt.Println()
}
