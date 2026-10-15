package models

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/tara-vision/taraengine/pkg/api"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
)

// EngineReport is what the engine says about itself and the machine.
type EngineReport struct {
	Version     string
	Runtime     string // the llama.cpp build the engine runs
	Platform    string
	Degraded    string // the health detail when the engine reports degraded, "" when it is ok
	Starting    string // the health detail while the engine is still getting ready, "" once it is
	Machine     api.Machine
	RuntimeNote string       // what is wrong with the runtime, "" when it is installed and verified
	Recommended string       // the engine's recommendation for the machine's tier, "" when it has none
	Faulty      *FaultyModel // the configured model's failure, when the engine marks it faulty
}

// FaultyModel is why the engine refuses a model until it is loaded with force.
type FaultyModel struct {
	ExitCode int
	LogTail  string
}

// engineClient is a client that hands out the engine's control API: taraengine's.
type engineClient interface {
	Engine() *taraengine.Engine
}

// diagnoseEngine fills the report from the engine: its health, version and doctor, then the models.
func (r *Report) diagnoseEngine(ctx context.Context, eng *taraengine.Engine) {
	rep, err := engineReport(ctx, eng)
	if err != nil {
		r.fail(err)
		return
	}
	if rep.Starting != "" { // the engine lists nothing while it starts; doctor reports the step and does not wait
		r.ServerOK, r.Engine = true, rep
		return
	}
	list, err := eng.Models(ctx)
	if err != nil {
		r.fail(err)
		return
	}
	r.ServerOK, r.Engine = true, rep
	r.collectEngineModels(ctx, eng, list)
}

// engineReport reads the engine's health, version and doctor. The version is required, being the first call that
// needs the key; a doctor that fails only leaves the machine lines out. The engine's text reaches the terminal, so
// its control characters are dropped.
func engineReport(ctx context.Context, eng *taraengine.Engine) (*EngineReport, error) {
	health, err := eng.Health(ctx)
	if err != nil {
		return nil, err
	}
	v, err := eng.Version(ctx)
	if err != nil {
		return nil, err
	}
	rep := &EngineReport{Version: llm.Printable(v.Version), Runtime: llm.Printable(v.Runtime.Build),
		Platform: llm.Printable(v.Platform)}
	switch health.Status {
	case api.HealthOK:
	case api.HealthStarting:
		rep.Starting = llm.Printable(health.Detail)
	default:
		rep.Degraded = llm.Printable(health.Detail)
	}
	if d, err := eng.Doctor(ctx); err == nil {
		rep.Machine, rep.RuntimeNote = printableMachine(d.Machine), llm.Printable(runtimeNote(d.Runtime))
		rep.Recommended = llm.Printable(d.Recommendation[d.Machine.Tier])
	}
	return rep, nil
}

// printableMachine is m with the control characters of its text dropped.
func printableMachine(m api.Machine) api.Machine {
	m.OS, m.Arch, m.GPU = llm.Printable(m.OS), llm.Printable(m.Arch), llm.Printable(m.GPU)
	m.Tier, m.DriverVersion = llm.Printable(m.Tier), llm.Printable(m.DriverVersion)
	return m
}

// installedModel is the engine's model as the doctor lists it. An import the engine has not loaded lists no
// capabilities and marks them inferred: they are unknown then, not absent.
func installedModel(m api.Model) InstalledModel {
	return InstalledModel{Name: m.Name, SizeGB: float64(m.Size) / 1e9, Context: m.NativeContext,
		Tools: slices.Contains(m.Capabilities, "tools"), Thinking: slices.Contains(m.Capabilities, "thinking"),
		Vision: slices.Contains(m.Capabilities, "vision"), Status: m.Status,
		CapabilitiesUnknown: m.CapabilitiesInferred && len(m.Capabilities) == 0}
}

// collectEngineModels lists the pulled models, the configured model's loaded window and, when the engine marks it
// faulty, its exit code and log tail.
func (r *Report) collectEngineModels(ctx context.Context, eng *taraengine.Engine, list []api.Model) {
	for _, m := range list {
		if m.Status == "available" {
			continue
		}
		r.Models = append(r.Models, installedModel(m))
	}
	sortModels(r.Models)
	if loaded, err := eng.Loaded(ctx); err == nil {
		for _, l := range loaded {
			if l.Name == r.ConfiguredModel {
				r.LoadedContext = l.Context
			}
		}
	}
	if m, ok := r.installed(r.ConfiguredModel); ok && m.Status == "faulty" {
		if d, err := eng.Model(ctx, m.Name); err == nil {
			// llama-server's log echoes an imported model's metadata: keep its control characters off the terminal
			r.Engine.Faulty = &FaultyModel{ExitCode: d.ExitCode, LogTail: llm.Printable(d.LogTail)}
		}
	}
}

// runtimeNote is what doctor says about the engine's llama.cpp runtime: nothing when it is installed and verified.
func runtimeNote(s api.RuntimeStatus) string {
	switch {
	case s.Error != "":
		return s.Error
	case !s.Installed:
		return "not installed: taraengine runtime install"
	case !s.Verified:
		return "installed but not verified: taraengine runtime install"
	}
	return ""
}

// renderServer writes the engine's lines: its version, runtime and platform, a degraded state, and a warning when it
// is older than this taracode supports.
func (e *EngineReport) renderServer(b *strings.Builder) {
	fmt.Fprintf(b, "          taraengine %s (runtime %s, %s)\n", e.Version, e.Runtime, e.Platform)
	if e.Degraded != "" {
		fmt.Fprintf(b, "          degraded: %s\n", e.Degraded)
	}
	if e.Starting != "" {
		fmt.Fprintf(b, "          starting: %s\n", e.Starting)
	}
	if versionBefore(e.Version, taraengine.MinVersion) {
		fmt.Fprintf(b, "          taracode supports taraengine %s or newer; update the engine\n", taraengine.MinVersion)
	}
}

// renderMachine writes the Machine line and, when the runtime has a problem, the Runtime line.
func (r *Report) renderMachine(b *strings.Builder) {
	if r.Engine == nil || r.Engine.Machine.OS == "" {
		return
	}
	m := r.Engine.Machine
	gpu := "no GPU"
	if m.GPU != "" {
		gpu = fmt.Sprintf("%s (%d GiB)", m.GPU, m.GPUMemoryBytes>>30)
	}
	fmt.Fprintf(b, "Machine   %s/%s, %s, %d GiB RAM, tier %s\n", m.OS, m.Arch, gpu, m.RAMBytes>>30, m.Tier)
	if r.Engine.RuntimeNote != "" {
		fmt.Fprintf(b, "Runtime   %s\n", r.Engine.RuntimeNote)
	}
}

// renderAdvice writes the engine's recommendation for this machine: that it is pulled, or how to pull it.
func (r *Report) renderAdvice(b *strings.Builder) {
	if r.Engine == nil || r.Engine.Recommended == "" {
		return
	}
	rec := r.Engine.Recommended
	if _, ok := r.installed(rec); ok {
		fmt.Fprintf(b, "Advice    %s is pulled and is the recommended model for this machine\n", rec)
		return
	}
	fmt.Fprintf(b, "Advice    recommended for tier %s: %s\n          taraengine pull %s\n", r.Engine.Machine.Tier,
		rec, rec)
}

// faulty is the configured model's failure when the engine marks it faulty.
func (r *Report) faulty() *FaultyModel {
	if r.Engine == nil {
		return nil
	}
	return r.Engine.Faulty
}
