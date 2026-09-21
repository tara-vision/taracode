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
	Tools                   map[string]string
	registry                *Registry
	RecommendationInstalled bool
}

// Diagnose collects everything the doctor prints. lookPath is exec.LookPath in production.
func Diagnose(
	ctx context.Context,
	client llm.Client,
	host string,
	ramGB int,
	configuredModel string,
	lookPath func(string) (string, error),
) Report {
	rep := Report{
		Host: host, RAMGB: ramGB, Tier: TierFor(ramGB), ConfiguredModel: configuredModel, Tools: map[string]string{},
	}
	rep.registry, _ = Load()
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
			_, m.InRegistry = r.registry.Find(info.Name)
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
		fmt.Fprintf(&b, "          Ollama %s reachable\n", r.ServerVersion)
	} else {
		fmt.Fprintf(&b, "          unreachable: %s\n", r.ServerError)
	}
	fmt.Fprintf(&b, "Machine   %d GB RAM, tier %s GB\n", r.RAMGB, r.Tier)
	r.renderModels(&b)
	if r.ConfiguredModel != "" {
		fmt.Fprintf(&b, "Model     %s", r.ConfiguredModel)
		if r.LoadedContext > 0 {
			fmt.Fprintf(&b, " (loaded with a %d-token context)", r.LoadedContext)
		}
		b.WriteString("\n")
	}
	if rec, ok := r.Recommendation(); ok {
		if r.RecommendationInstalled {
			fmt.Fprintf(&b, "Advice    %s is installed and is the recommended model for this machine\n", rec.Name)
		} else {
			fmt.Fprintf(&b, "Advice    recommended for %d GB: %s (%.0f GB download)\n          ollama pull %s\n",
				r.RAMGB, rec.Name, rec.DownloadGB, rec.Name)
		}
	}
	r.renderTools(&b)
	return b.String()
}

// renderModels writes the installed-models section, one row per model with its capability list.
func (r *Report) renderModels(b *strings.Builder) {
	if len(r.Models) == 0 {
		return
	}
	b.WriteString("Models\n")
	for _, m := range r.Models {
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
		fmt.Fprintf(b, "          %-28s %5.1f GB  ctx %-7d %s\n", m.Name, m.SizeGB, m.Context, strings.Join(caps, " "))
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
