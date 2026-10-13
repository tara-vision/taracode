// Package models is taracode's doctor: the server, the models and the external tools.
package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
)

// externalTools are the CLIs taracode's tools shell out to.
var externalTools = []string{
	"kubectl", "helm", "terraform", "docker", "aws", "az", "gcloud", "git", "trivy", "gitleaks", "tfsec",
}

// InstalledModel is one model on the server as the doctor sees it.
type InstalledModel struct {
	Name     string
	SizeGB   float64
	Context  int // the native window, 0 when the server does not say
	Tools    bool
	Thinking bool
	Vision   bool
	Status   string // the engine's word for the model's state ("pulled", "loaded", "sleeping", "faulty"); "" elsewhere

	// CapabilitiesUnknown is an import the engine has not loaded: it infers the capabilities at the load and lists none
	// before that, so Tools, Thinking and Vision say nothing about the model.
	CapabilitiesUnknown bool
}

// Report is the result of Diagnose.
type Report struct {
	Host            string
	ServerOK        bool
	ServerVersion   string // an OpenAI-compatible server's own version, "" when it has none
	ServerError     string
	Refused         bool          // the server answered and refused (*llm.ServerError); otherwise nothing answered
	InstallHint     bool          // nothing answered where taracode looks for an engine: say how to install one
	KeyNeeded       bool          // the engine refused taracode's key
	Engine          *EngineReport // set when the server is a taraengine
	Models          []InstalledModel
	ConfiguredModel string
	LoadedContext   int
	ContextWindow   int    // the window taracode will load ConfiguredModel at; 0 = not resolved
	ContextNote     string // a warning from resolving ContextWindow, if any
	KeepAliveSet    bool   // keep_alive is set in the config, where it no longer does anything
	Tools           map[string]string
	PolicyNote      string // which policy files load, or the parse error; "" = not checked
}

// Diagnose collects everything the doctor prints. lookPath is exec.LookPath in production. resolveWindow resolves the
// window taracode will load the configured model at, on an engine; nil skips that line. keepAliveSet adds the note
// that keep_alive does nothing.
func Diagnose(
	ctx context.Context,
	client llm.Client,
	host, configuredModel string,
	keepAliveSet bool,
	lookPath func(string) (string, error),
	resolveWindow func(modelMax int) (window int, note string),
) Report {
	rep := newReport(host, configuredModel, lookPath)
	rep.KeepAliveSet = keepAliveSet
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if ec, ok := client.(engineClient); ok {
		rep.diagnoseEngine(ctx, ec.Engine())
		rep.resolveConfiguredWindow(resolveWindow)
	} else {
		rep.diagnoseGeneric(ctx, client)
	}
	return rep
}

// UnreachableReport is the report for a host detection found nothing at: the error, the configured model and the
// tools, so doctor still prints all it can before it exits 1.
func UnreachableReport(host string, err error, configuredModel string, lookPath func(string) (string, error)) Report {
	rep := newReport(host, configuredModel, lookPath)
	rep.fail(err)
	return rep
}

// newReport is an empty report with the external tools looked up.
func newReport(host, configuredModel string, lookPath func(string) (string, error)) Report {
	rep := Report{Host: host, ConfiguredModel: configuredModel, Tools: map[string]string{}}
	for _, name := range externalTools {
		path, err := lookPath(name)
		if err != nil {
			path = ""
		}
		rep.Tools[name] = path
	}
	return rep
}

// fail records why taracode cannot use the server. Nothing answering where taracode looks for an engine also gets
// the install line, and a refused key the line that says where the key goes.
func (r *Report) fail(err error) {
	r.ServerOK, r.ServerError = false, err.Error()
	var unreachable *llm.UnreachableError
	var refused *llm.ServerError
	r.InstallHint = errors.As(err, &unreachable) && unreachable.Hint != ""
	r.Refused = errors.As(err, &refused)
	r.KeyNeeded = r.Refused && refused.Status == http.StatusUnauthorized
}

// diagnoseGeneric checks an OpenAI-compatible server: its version when it has one, and the models it lists.
func (r *Report) diagnoseGeneric(ctx context.Context, client llm.Client) {
	version, err := client.Version(ctx)
	if err != nil {
		r.fail(err)
		return
	}
	infos, err := client.Models(ctx)
	if err != nil {
		r.fail(err)
		return
	}
	r.ServerOK, r.ServerVersion = true, version
	for _, info := range infos {
		r.Models = append(r.Models, InstalledModel{Name: info.Name, SizeGB: float64(info.Size) / 1e9})
	}
	sortModels(r.Models)
}

func sortModels(models []InstalledModel) {
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
}

// installed is the listed model with that name.
func (r *Report) installed(name string) (InstalledModel, bool) {
	for _, m := range r.Models {
		if m.Name == name {
			return m, true
		}
	}
	return InstalledModel{}, false
}

// resolveConfiguredWindow sets ContextWindow and ContextNote once the configured model is found among the pulled
// models, whose native window resolveWindow needs.
func (r *Report) resolveConfiguredWindow(resolveWindow func(modelMax int) (int, string)) {
	if resolveWindow == nil {
		return
	}
	if m, ok := r.installed(r.ConfiguredModel); ok {
		r.ContextWindow, r.ContextNote = resolveWindow(m.Context)
	}
}

// Render formats the report for the terminal. The value receiver lets it be called on a Diagnose result directly.
func (r Report) Render() string {
	var b strings.Builder
	r.renderServer(&b)
	r.renderMachine(&b)
	r.renderModels(&b)
	r.renderModelAndContext(&b)
	if r.KeepAliveSet {
		b.WriteString("Keep      keep_alive in the config does nothing: taracode sends it to no server\n")
	}
	if r.PolicyNote != "" {
		fmt.Fprintf(&b, "Policy    %s\n", r.PolicyNote)
	}
	r.renderAdvice(&b)
	r.renderTools(&b)
	return b.String()
}

// renderServer writes the Server lines: the host, then the engine, the OpenAI-compatible server, or why taracode
// cannot use it.
func (r *Report) renderServer(b *strings.Builder) {
	fmt.Fprintf(b, "Server    %s\n", llm.RedactedHost(r.Host))
	switch {
	case !r.ServerOK && r.Refused:
		fmt.Fprintf(b, "          refused: %s\n", r.ServerError)
		if r.KeyNeeded {
			b.WriteString("          set key in ~/.taracode/config.yaml, or pass --key\n")
		}
	case !r.ServerOK:
		fmt.Fprintf(b, "          unreachable: %s\n", r.ServerError)
		if r.InstallHint {
			fmt.Fprintf(b, "          install it: %s\n", taraengine.InstallHint)
		}
	case r.Engine != nil:
		r.Engine.renderServer(b)
	case r.ServerVersion != "":
		fmt.Fprintf(b, "          %s reachable\n", r.ServerVersion)
	default:
		b.WriteString("          server reachable\n")
	}
}

// renderModels writes the models section, one row per model: size, native window, capabilities and, on an engine,
// the model's state.
func (r *Report) renderModels(b *strings.Builder) {
	if len(r.Models) == 0 {
		return
	}
	b.WriteString("Models\n")
	for _, m := range r.Models {
		size := fmt.Sprintf("%5.1f GB  ctx %-7d ", m.SizeGB, m.Context)
		if m.SizeGB == 0 && m.Context == 0 {
			size = "" // an OpenAI-compatible server lists names only
		}
		line := fmt.Sprintf("          %-28s %s%-22s %s", m.Name, size, m.capabilityColumn(), m.Status)
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
}

// capabilityColumn is what the models section says of the model's capabilities: the ones it has, or that they are
// not known yet.
func (m InstalledModel) capabilityColumn() string {
	if m.CapabilitiesUnknown {
		return "unknown until loaded"
	}
	var caps []string
	for _, c := range []struct {
		on   bool
		name string
	}{{m.Tools, "tools"}, {m.Thinking, "thinking"}, {m.Vision, "vision"}} {
		if c.on {
			caps = append(caps, c.name)
		}
	}
	return strings.Join(caps, " ")
}

// renderModelAndContext writes the Model line, a faulty model's exit code and log tail, and the window taracode will
// load the model at.
func (r *Report) renderModelAndContext(b *strings.Builder) {
	if r.ConfiguredModel != "" {
		fmt.Fprintf(b, "Model     %s%s\n", r.ConfiguredModel, r.modelNote())
	}
	if f := r.faulty(); f != nil {
		fmt.Fprintf(b, "          faulty, exit code %d: taraengine load %s --force tries it again\n", f.ExitCode,
			r.ConfiguredModel)
		for _, line := range strings.Split(strings.TrimSpace(f.LogTail), "\n") {
			fmt.Fprintf(b, "          | %s\n", line)
		}
	}
	if r.ContextWindow > 0 {
		fmt.Fprintf(b, "Context   %d tokens, set when the model loads%s\n", r.ContextWindow, r.reloadNote())
		if r.ContextNote != "" {
			fmt.Fprintf(b, "          %s\n", r.ContextNote)
		}
	}
}

// modelNote is what follows the configured model's name: why taracode cannot use it, or the window it runs at. An
// import the engine has not loaded is no model without tools: the session starts it, and its capabilities show once
// it loads.
func (r *Report) modelNote() string {
	m, installed := r.installed(r.ConfiguredModel)
	switch {
	case r.Engine != nil && !installed:
		return fmt.Sprintf(" (not pulled: taraengine pull %s)", r.ConfiguredModel)
	case m.CapabilitiesUnknown:
		return " (capabilities show once it loads)"
	case r.Engine != nil && !m.Tools:
		return " (no tool calls: taracode needs a model that has them)"
	case r.LoadedContext > 0 && m.Status == "sleeping": // the engine lists a sleeping model among the loaded ones
		return fmt.Sprintf(" (sleeping at a %d-token window)", r.LoadedContext)
	case r.LoadedContext > 0:
		return fmt.Sprintf(" (loaded at a %d-token window)", r.LoadedContext)
	}
	return ""
}

// reloadNote says that the next session reloads the model when it runs at another window than taracode will set.
func (r *Report) reloadNote() string {
	if r.LoadedContext > 0 && r.LoadedContext != r.ContextWindow {
		return "; the next session reloads it"
	}
	return ""
}

// renderTools writes the external-tools section, one line per tool taracode's tools shell out to.
func (r *Report) renderTools(b *strings.Builder) {
	b.WriteString("Tools\n")
	names := make([]string, 0, len(r.Tools))
	for n := range r.Tools {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if r.Tools[n] == "" {
			fmt.Fprintf(b, "          %s: not found\n", n)
		} else {
			fmt.Fprintf(b, "          %s: %s\n", n, r.Tools[n])
		}
	}
}
