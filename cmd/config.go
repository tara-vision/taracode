package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cast"
	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/policy"
)

// setDefaults registers the v3 defaults (spec 5.9). Keys that 2.x users may still have keep working
// through loadOptions' migrations.
func setDefaults() {
	viper.SetDefault("generation.temperature", 0.7)
	viper.SetDefault("generation.top_p", 0.9)
	viper.SetDefault("generation.num_predict", 0)
	viper.SetDefault("context.window", "auto")
	viper.SetDefault("context.max_tool_output_lines", 500)
	viper.SetDefault("context.max_tool_output_chars", 15000)
	viper.SetDefault("context.max_tool_iterations", 20)
	viper.SetDefault("context.compaction_enabled", true)
	viper.SetDefault("context.compaction_threshold", 0.75)
	viper.SetDefault("context.compaction_keep_recent", 4)
	viper.SetDefault("think", "auto")
	viper.SetDefault("keep_alive", "")
	viper.SetDefault("mode", "")
	viper.SetDefault("offline", false)
	viper.SetDefault("scan.default_severity", "")
	viper.SetDefault("search.primary", "duckduckgo")
	viper.SetDefault("search.fallback", "searxng")
	viper.SetDefault("search.timeout", "10s")
	viper.SetDefault("search.retry_count", 1)
	viper.SetDefault("search.searxng_instance", "")
	viper.SetDefault("memory.enabled", true)
	viper.SetDefault("memory.max_memories", 500)
	viper.SetDefault("memory.max_context_tokens", 2000)
	viper.SetDefault("memory.retention_days", 90)
	viper.SetDefault("memory.auto_capture", true)
	viper.SetDefault("upgrade.auto_check", true)
	viper.SetDefault("upgrade.auto_upgrade", false)
	viper.SetDefault("upgrade.show_changelog", true)
	viper.SetDefault("mcp.enabled", true)
	viper.SetDefault("max_context_tokens", 32768)
	viper.SetDefault("show_context_budget", true)
	viper.SetDefault("preview_edits", true)
	viper.SetDefault("preview_threshold", 0)
	viper.SetDefault("no_stream_commands", false)
}

// loadOptions builds the assistant options from viper and returns the migration warnings to print
// once at startup.
func loadOptions() (agent.Options, []string) {
	opts := agent.DefaultOptions()
	var warnings []string
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	opts.Host = resolveHost()
	opts.APIKey = viper.GetString("key")
	opts.Vendor = viper.GetString("vendor")
	opts.Streaming = !viper.GetBool("no_stream")
	opts.Spinner = !viper.GetBool("no_spinner")
	opts.Offline = viper.GetBool("offline")

	gen := agent.ModelOptions{
		Temperature: float32(viper.GetFloat64("generation.temperature")),
		TopP:        float32(viper.GetFloat64("generation.top_p")),
		NumPredict:  viper.GetInt("generation.num_predict"),
	}
	switch v := viper.Get("model").(type) {
	case string:
		opts.Model = v
	case map[string]any:
		// The 2.x layout: model: is a section. Its values win over the generation: defaults.
		// Once --model is bound to the "model" key (v3), viper 1.21 shadows every nested
		// "model.<key>" lookup unconditionally (isPathShadowedInFlatMap fires whether or not the
		// flag was actually passed on the CLI), so the values come out of this map directly
		// rather than through viper.Get*("model.<key>") (ruling P2-R17).
		warn("config: model: is a section in the 2.x layout; move temperature, top_p and num_predict " +
			"under generation: and set model: <name>")
		if val, ok := v["temperature"]; ok {
			gen.Temperature = float32(cast.ToFloat64(val))
		}
		if val, ok := v["top_p"]; ok {
			gen.TopP = float32(cast.ToFloat64(val))
		}
		if val, ok := v["num_predict"]; ok {
			gen.NumPredict = cast.ToInt(val)
		}
	}
	opts.Generation = gen
	if m := viper.GetString("mode"); m != "" {
		if mode, ok := policy.ParseMode(m); ok {
			opts.DefaultMode = mode
		} else if m == "devops" || m == "security" {
			warn("config: mode %q is from 2.x; modes are investigate and operate now (starting in investigate)", m)
		} else {
			warn("config: unknown mode %q (investigate or operate); starting in investigate", m)
		}
	}
	if rootCmd.PersistentFlags().Changed("mode") {
		opts.Mode = opts.DefaultMode
	}
	opts.Tools.DefaultSeverity = viper.GetString("scan.default_severity")
	if legacy := viper.GetString("security.default_severity"); legacy != "" && opts.Tools.DefaultSeverity == "" {
		opts.Tools.DefaultSeverity = legacy
		warn("config: security.default_severity is scan.default_severity now")
	}
	for _, section := range []string{"agents", "watch"} {
		if viper.IsSet(section) {
			warn("config: the %s: section is ignored since 3.0.0-alpha.2", section)
		}
	}
	if viper.IsSet("hosts") || viper.IsSet("default_host") {
		if viper.GetString("host") == "" && opts.Host != "" {
			warn("config: hosts: and default_host: are ignored since 3.1.0; using %s from the retired section as host: "+
				"for this run, set host: to keep it", opts.Host)
		} else {
			warn("config: hosts: and default_host: are ignored since 3.1.0; taracode talks to the one host in host:")
		}
	}
	opts.Think = viper.GetString("think")
	opts.KeepAlive = viper.GetString("keep_alive")
	opts.ContextWindow = viper.GetString("context.window")
	opts.MaxContextTokens = viper.GetInt("max_context_tokens")
	opts.Truncation = agent.TruncationConfig{
		MaxLines: viper.GetInt("context.max_tool_output_lines"),
		MaxChars: viper.GetInt("context.max_tool_output_chars"),
	}
	opts.Compaction = agent.CompactionConfig{
		Enabled:    viper.GetBool("context.compaction_enabled") && !viper.GetBool("context.no_compaction"),
		Threshold:  viper.GetFloat64("context.compaction_threshold"),
		KeepRecent: viper.GetInt("context.compaction_keep_recent"),
		MaxTokens:  viper.GetInt("max_context_tokens"),
	}
	opts.MaxIterations = viper.GetInt("context.max_tool_iterations")
	opts.PreviewEdits = viper.GetBool("preview_edits")
	opts.PreviewThreshold = viper.GetInt("preview_threshold")
	opts.MemoryEnabled = viper.GetBool("memory.enabled")
	opts.MemoryMaxTokens = viper.GetInt("memory.max_context_tokens")
	return opts, warnings
}

// printWarnings shows the migration warnings once.
func printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Println("  " + strings.TrimSpace(w))
	}
}

// resolveHost is the one host taracode talks to: host: (--host, TARACODE_HOST), or, when that is
// empty, the default host of a 3.0 hosts: section so such a file keeps starting after the pool's
// removal in 3.1.0.
func resolveHost() string {
	if host := viper.GetString("host"); host != "" {
		return host
	}
	url, _ := legacyHostURL()
	return url
}

// legacyHostURL is the url of the default host of a retired hosts: section: default_host when it
// names one, otherwise the host with the lowest priority (unset counts as 10, ties by name). ok is
// false when the section names no usable url.
func legacyHostURL() (url string, ok bool) {
	hosts := viper.GetStringMap("hosts")
	if len(hosts) == 0 {
		return "", false
	}
	pick := viper.GetString("default_host")
	if _, known := hosts[pick]; !known {
		names := make([]string, 0, len(hosts))
		for name := range hosts {
			names = append(names, name)
		}
		sort.Strings(names)
		pick, best := "", 0
		for _, name := range names {
			priority := 10
			if key := "hosts." + name + ".priority"; viper.IsSet(key) {
				priority = viper.GetInt(key)
			}
			if pick == "" || priority < best {
				pick, best = name, priority
			}
		}
		url = viper.GetString("hosts." + pick + ".url")
		return url, url != ""
	}
	url = viper.GetString("hosts." + pick + ".url")
	return url, url != ""
}

// noHost is the error every entry point returns when no host is configured. A retired hosts:
// section gets a pointer to host:, the one migration a 3.0 file needs.
func noHost() error {
	msg := "LLM server host not found; set --host, TARACODE_HOST, or host: in ~/.taracode/config.yaml"
	if viper.IsSet("hosts") {
		msg += " (hosts: is ignored since 3.1.0; put your default host's url in host:)"
	}
	return errors.New(msg)
}
