package models

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/llm"
)

// externalTools are the CLIs taracode's tools shell out to.
var externalTools = []string{
	"kubectl", "helm", "terraform", "docker", "aws", "az", "gcloud", "git", "trivy", "gitleaks", "tfsec",
}

// InstalledModel is one model on the server as the doctor sees it.
type InstalledModel struct {
	Name       string
	SizeGB     float64
	Context    int
	Tools      bool
	Thinking   bool
	Vision     bool
	InRegistry bool
	MinOllama  string // from the registry entry, when InRegistry; "" otherwise.
}

// Report is the result of Diagnose.
type Report struct {
	Host                    string
	ServerOK                bool
	ServerVersion           string
	ServerError             string
	RAMGB                   int
	Tier                    Tier
	Models                  []InstalledModel
	ConfiguredModel         string
	LoadedContext           int
	ContextWindow           int    // num_ctx that will be requested for ConfiguredModel; 0 = not resolved.
	ContextNote             string // a warning from resolving ContextWindow, if any.
	Tools                   map[string]string
	RegistryError           string // set when the embedded registry failed to load; "" otherwise.
	registry                *Registry
	RecommendationInstalled bool
	PolicyNote              string // which policy files load, or the parse error; "" = not checked.
}

// Diagnose collects everything the doctor prints. lookPath is exec.LookPath in production.
// resolveWindow resolves the context window that will be requested for the configured model, once
// it is known to be installed; nil skips that section (the OpenAI-compatible path has no way to
// set num_ctx, so callers there pass nil).
func Diagnose(
	ctx context.Context,
	client llm.Client,
	host string,
	ramGB int,
	configuredModel string,
	lookPath func(string) (string, error),
	resolveWindow func(modelMax int) (window int, note string),
) Report {
	rep := Report{
		Host: host, RAMGB: ramGB, Tier: TierFor(ramGB), ConfiguredModel: configuredModel, Tools: map[string]string{},
	}
	if registry, err := Load(); err != nil {
		rep.RegistryError = err.Error()
	} else {
		rep.registry = registry
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	version, err := client.Version(ctx)
	if err != nil {
		rep.ServerError = err.Error()
	} else {
		rep.ServerOK = true
		rep.ServerVersion = version
		rep.collectModels(ctx, client)
	}
	for _, name := range externalTools {
		if path, err := lookPath(name); err == nil {
			rep.Tools[name] = path
		} else {
			rep.Tools[name] = ""
		}
	}
	if rec, ok := rep.Recommendation(); ok {
		for _, m := range rep.Models {
			if m.Name == rec.Name {
				rep.RecommendationInstalled = true
			}
		}
	}
	rep.resolveConfiguredWindow(resolveWindow)
	return rep
}

// collectModels fills in the installed-models section and the loaded context, once Version has
// answered. Version is a no-op stub on the OpenAI-compatible transport (vLLM, llama.cpp), so a
// Models failure here is the first real signal such a server is unreachable: ServerOK is cleared
// too, not just ServerError, or the report would call a dead vLLM host "reachable".
func (r *Report) collectModels(ctx context.Context, client llm.Client) {
	infos, err := client.Models(ctx)
	if err != nil {
		r.ServerError = err.Error()
		r.ServerOK = false
		return
	}
	for _, info := range infos {
		m := InstalledModel{Name: info.Name, SizeGB: float64(info.Size) / 1e9}
		if details, err := client.Show(ctx, info.Name); err == nil {
			m.Context = details.ContextLength
			m.Tools = details.Has("tools")
			m.Thinking = details.Has("thinking")
			m.Vision = details.Has("vision")
		}
		if r.registry != nil {
			if entry, ok := r.registry.Find(info.Name); ok {
				m.InRegistry = true
				m.MinOllama = entry.MinOllama
			}
		}
		r.Models = append(r.Models, m)
	}
	sort.Slice(r.Models, func(i, j int) bool { return r.Models[i].Name < r.Models[j].Name })
	if loaded, err := client.Loaded(ctx); err == nil {
		for _, l := range loaded {
			if l.Name == r.ConfiguredModel || l.Name == r.ConfiguredModel+":latest" {
				r.LoadedContext = l.ContextLength
			}
		}
	}
}

// resolveConfiguredWindow sets ContextWindow and ContextNote from resolveWindow, once the
// configured model is found among the installed models (its native context is what resolveWindow
// needs). A nil resolveWindow, an empty ConfiguredModel, or no matching installed model leaves
// both fields at their zero value.
func (r *Report) resolveConfiguredWindow(resolveWindow func(modelMax int) (int, string)) {
	if resolveWindow == nil || r.ConfiguredModel == "" {
		return
	}
	for _, m := range r.Models {
		if m.Name == r.ConfiguredModel || m.Name == r.ConfiguredModel+":latest" {
			r.ContextWindow, r.ContextNote = resolveWindow(m.Context)
			return
		}
	}
}

// Recommendation is the registry default for the host's tier.
func (r *Report) Recommendation() (Entry, bool) {
	if r.registry == nil {
		return Entry{}, false
	}
	e := r.registry.DefaultForTier(r.Tier)
	return e, e.Name != ""
}

// Render formats the report for the terminal.
func (r *Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Server    %s\n", r.Host)
	if r.ServerOK {
		if r.ServerVersion != "" {
			fmt.Fprintf(&b, "          Ollama %s reachable\n", r.ServerVersion)
		} else {
			b.WriteString("          server reachable\n")
		}
	} else {
		fmt.Fprintf(&b, "          unreachable: %s\n", r.ServerError)
	}
	if r.RegistryError != "" {
		fmt.Fprintf(&b, "Registry  unavailable: %s\n", r.RegistryError)
	}
	fmt.Fprintf(&b, "Machine   %d GB RAM, tier %s GB\n", r.RAMGB, r.Tier)
	r.renderModels(&b)
	r.renderModelAndContext(&b)
	if r.PolicyNote != "" {
		fmt.Fprintf(&b, "Policy    %s\n", r.PolicyNote)
	}
	r.renderAdvice(&b)
	r.renderTools(&b)
	return b.String()
}

// renderModels writes the installed-models section, one row per model.
func (r *Report) renderModels(b *strings.Builder) {
	if len(r.Models) == 0 {
		return
	}
	b.WriteString("Models\n")
	for _, m := range r.Models {
		r.renderModel(b, m)
	}
}

// renderModel writes one model's row: name, size, native context, capabilities, registry
// membership, and (when the server is older than the model needs) an Ollama-version warning.
func (r *Report) renderModel(b *strings.Builder, m InstalledModel) {
	caps := []string{}
	if m.Tools {
		caps = append(caps, "tools")
	}
	if m.Thinking {
		caps = append(caps, "thinking")
	}
	if m.Vision {
		caps = append(caps, "vision")
	}
	registryMark := "not in registry"
	if m.InRegistry {
		registryMark = "registry"
	}
	fmt.Fprintf(b, "          %-28s %5.1f GB  ctx %-7d %-22s %s",
		m.Name, m.SizeGB, m.Context, strings.Join(caps, " "), registryMark)
	if r.ServerVersion != "" && m.MinOllama != "" && versionBefore(r.ServerVersion, m.MinOllama) {
		fmt.Fprintf(b, " needs Ollama >= %s", m.MinOllama)
	}
	b.WriteString("\n")
}

// renderModelAndContext writes the Model line (the configured model and its loaded context, when
// known) and, right after it, the context window that will be requested for that model.
func (r *Report) renderModelAndContext(b *strings.Builder) {
	if r.ConfiguredModel != "" {
		fmt.Fprintf(b, "Model     %s", r.ConfiguredModel)
		if r.LoadedContext > 0 {
			fmt.Fprintf(b, " (loaded with a %d-token context)", r.LoadedContext)
		}
		b.WriteString("\n")
	}
	if r.ContextWindow > 0 {
		fmt.Fprintf(b, "Context   %d tokens will be requested per turn\n", r.ContextWindow)
		if r.ContextNote != "" {
			fmt.Fprintf(b, "          %s\n", r.ContextNote)
		}
	}
}

// renderAdvice writes the Advice line: whether the recommended model for this tier is already
// installed, and (when the server is older than the recommendation needs) an Ollama-version note.
func (r *Report) renderAdvice(b *strings.Builder) {
	rec, ok := r.Recommendation()
	if !ok {
		return
	}
	if r.RecommendationInstalled {
		fmt.Fprintf(b, "Advice    %s is installed and is the recommended model for this machine\n", rec.Name)
	} else {
		fmt.Fprintf(b, "Advice    recommended for %d GB: %s (%.0f GB download)\n          ollama pull %s\n",
			r.RAMGB, rec.Name, rec.DownloadGB, rec.Name)
	}
	if rec.MinOllama != "" && r.ServerVersion != "" && versionBefore(r.ServerVersion, rec.MinOllama) {
		fmt.Fprintf(b, "          needs Ollama >= %s (server has %s)\n", rec.MinOllama, r.ServerVersion)
	}
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
