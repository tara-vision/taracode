package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/mcp"
	"github.com/tara-vision/taracode/internal/provider"
)

var (
	cfgFile       string
	host          string
	apiKey        string
	model         string
	vendor        string
	mode          string
	severity      string
	noStream      bool
	noSpinner     bool
	verboseErrors bool
	maxToolOutput int
	maxIterations int
	noCompaction  bool
	Version       = "dev"
)

// SearchConfig holds search-related configuration
type SearchConfig struct {
	Primary         string `mapstructure:"primary"`
	Fallback        string `mapstructure:"fallback"`
	Timeout         string `mapstructure:"timeout"`
	RetryCount      int    `mapstructure:"retry_count"`
	SearXNGInstance string `mapstructure:"searxng_instance"`
	BraveAPIKey     string `mapstructure:"brave_api_key"`
}

// GetSearchConfig returns the search configuration from viper
func GetSearchConfig() SearchConfig {
	// Try nested search.brave_api_key first, fall back to top-level brave_api_key
	braveAPIKey := viper.GetString("search.brave_api_key")
	if braveAPIKey == "" {
		braveAPIKey = viper.GetString("brave_api_key")
	}

	return SearchConfig{
		Primary:         viper.GetString("search.primary"),
		Fallback:        viper.GetString("search.fallback"),
		Timeout:         viper.GetString("search.timeout"),
		RetryCount:      viper.GetInt("search.retry_count"),
		SearXNGInstance: viper.GetString("search.searxng_instance"),
		BraveAPIKey:     braveAPIKey,
	}
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	DefaultSeverity string `mapstructure:"default_severity"`
}

// GetSecurityConfig returns the security configuration from viper
func GetSecurityConfig() SecurityConfig {
	return SecurityConfig{
		DefaultSeverity: viper.GetString("security.default_severity"),
	}
}

// GetMCPConfig returns the MCP configuration from viper
func GetMCPConfig() mcp.MCPConfig {
	var cfg mcp.MCPConfig
	cfg.Enabled = viper.GetBool("mcp.enabled")

	// Get servers from config
	var servers []map[string]interface{}
	if err := viper.UnmarshalKey("mcp.servers", &servers); err == nil {
		for _, s := range servers {
			serverCfg := mcp.MCPServerConfig{}

			if name, ok := s["name"].(string); ok {
				serverCfg.Name = name
			}
			if command, ok := s["command"].(string); ok {
				serverCfg.Command = command
			}
			if args, ok := s["args"].([]interface{}); ok {
				serverCfg.Args = make([]string, len(args))
				for i, arg := range args {
					if str, ok := arg.(string); ok {
						serverCfg.Args[i] = str
					}
				}
			}
			if env, ok := s["env"].(map[string]interface{}); ok {
				serverCfg.Env = make(map[string]string)
				for k, v := range env {
					if str, ok := v.(string); ok {
						serverCfg.Env[k] = str
					}
				}
			}
			if autoConnect, ok := s["auto_connect"].(bool); ok {
				serverCfg.AutoConnect = autoConnect
			}
			if timeout, ok := s["timeout"].(string); ok {
				if d, err := time.ParseDuration(timeout); err == nil {
					serverCfg.Timeout = d
				}
			}

			if serverCfg.Name != "" && serverCfg.Command != "" {
				cfg.Servers = append(cfg.Servers, serverCfg)
			}
		}
	}

	return cfg
}

// GetHostsConfig returns the hosts configuration from viper
// Supports both new multi-host config and legacy single-host config
func GetHostsConfig() provider.HostsConfig {
	// Check for new hosts configuration
	if viper.IsSet("hosts") {
		cfg := provider.NewHostsConfig()

		// Get hosts map
		var hostsMap map[string]interface{}
		if err := viper.UnmarshalKey("hosts", &hostsMap); err == nil {
			for name, hostData := range hostsMap {
				if h, ok := hostData.(map[string]interface{}); ok {
					hostCfg := provider.HostConfig{
						Name:     name,
						Priority: 10, // default priority
					}

					if url, ok := h["url"].(string); ok {
						hostCfg.URL = url
					}
					if vendor, ok := h["vendor"].(string); ok {
						hostCfg.Vendor = vendor
					}
					if apiKey, ok := h["api_key"].(string); ok {
						hostCfg.APIKey = apiKey
					}
					if fallback, ok := h["fallback"].(string); ok {
						hostCfg.Fallback = fallback
					}
					switch p := h["priority"].(type) {
					case int:
						hostCfg.Priority = p
					case float64:
						hostCfg.Priority = int(p)
					}
					if timeout, ok := h["timeout"].(string); ok {
						if d, err := time.ParseDuration(timeout); err == nil {
							hostCfg.Timeout = d
						}
					}
					if models, ok := h["models"].([]interface{}); ok {
						for _, m := range models {
							if model, ok := m.(string); ok {
								hostCfg.Models = append(hostCfg.Models, model)
							}
						}
					}

					if hostCfg.URL != "" {
						cfg.Hosts[name] = hostCfg
					}
				}
			}
		}

		// Get default host
		cfg.DefaultHost = viper.GetString("default_host")
		if cfg.DefaultHost == "" && len(cfg.Hosts) > 0 {
			// If no default specified, use first host by priority
			minPriority := 999
			for name, h := range cfg.Hosts {
				if h.Priority < minPriority {
					minPriority = h.Priority
					cfg.DefaultHost = name
				}
			}
		}

		return cfg
	}

	// Fall back to legacy single-host configuration
	host := viper.GetString("host")
	apiKey := viper.GetString("key")
	vendor := viper.GetString("vendor")

	if host == "" {
		return provider.NewHostsConfig()
	}

	return provider.NewHostsConfigFromLegacy(host, apiKey, vendor)
}

// ValidSeverityLevels defines the valid severity levels for security scans
var ValidSeverityLevels = []string{"UNKNOWN", "LOW", "MEDIUM", "HIGH", "CRITICAL"}

// IsValidSeverity checks if a severity string contains only valid levels
func IsValidSeverity(severity string) bool {
	if severity == "" {
		return true
	}
	levels := strings.Split(strings.ToUpper(severity), ",")
	for _, level := range levels {
		level = strings.TrimSpace(level)
		valid := false
		for _, validLevel := range ValidSeverityLevels {
			if level == validLevel {
				valid = true
				break
			}
		}
		if !valid {
			return false
		}
	}
	return true
}

var rootCmd = &cobra.Command{
	Use:     "taracode",
	Version: Version,
	Short:   "Tara Code - DevOps & Cloud AI Assistant",
	Long: `Tara Code is an AI-powered CLI assistant specialized in DevOps, Cloud Infrastructure,
and Site Reliability Engineering. Expert guidance for Kubernetes, Terraform, Docker, and
multi-cloud deployments (AWS, Azure, GCP).`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate severity flag if provided
		severityValue := viper.GetString("security.default_severity")
		if severityValue != "" && !IsValidSeverity(severityValue) {
			return fmt.Errorf("invalid severity filter: %s\nValid levels: %s",
				severityValue, strings.Join(ValidSeverityLevels, ", "))
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Start interactive REPL mode
		startREPL()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.taracode/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&host, "host", "", "LLM server URL (e.g., http://localhost:11434)")
	rootCmd.PersistentFlags().StringVar(&apiKey, "key", "", "API key (optional for local servers)")
	rootCmd.PersistentFlags().StringVar(&model, "model", "", "model name (optional, auto-detected from server)")
	rootCmd.PersistentFlags().StringVar(&vendor, "vendor", "", "LLM vendor (auto, vllm, ollama, llama.cpp)")
	rootCmd.PersistentFlags().StringVar(&mode, "mode", "", "operating mode (devops, security)")
	rootCmd.PersistentFlags().StringVar(&severity, "severity", "", "default severity filter for security scans (e.g., HIGH,CRITICAL)")
	rootCmd.PersistentFlags().BoolVar(&noStream, "no-stream", false, "disable streaming output (show response all at once)")
	rootCmd.PersistentFlags().BoolVar(&noSpinner, "no-spinner", false, "disable spinner animations")
	rootCmd.PersistentFlags().BoolVar(&verboseErrors, "verbose-errors", false, "show detailed failure diagnostics with suggestions")
	rootCmd.PersistentFlags().IntVar(&maxToolOutput, "max-tool-output", 0, "max lines per tool output (0 = use config default)")
	rootCmd.PersistentFlags().IntVar(&maxIterations, "max-iterations", 0, "max tool call iterations per message (0 = use config default)")
	rootCmd.PersistentFlags().BoolVar(&noCompaction, "no-compaction", false, "disable automatic conversation compaction")

	viper.BindPFlag("host", rootCmd.PersistentFlags().Lookup("host"))
	viper.BindPFlag("key", rootCmd.PersistentFlags().Lookup("key"))
	// Note: "model" is NOT bound to viper because config.yaml uses "model:" as a
	// section (model.temperature, model.top_p, etc). Binding "model" to a pflag
	// shadows nested model.* keys in viper. The --model flag value is read directly
	// from the package-level variable instead.
	viper.BindPFlag("vendor", rootCmd.PersistentFlags().Lookup("vendor"))
	viper.BindPFlag("mode", rootCmd.PersistentFlags().Lookup("mode"))
	viper.BindPFlag("security.default_severity", rootCmd.PersistentFlags().Lookup("severity"))
	viper.BindPFlag("no_stream", rootCmd.PersistentFlags().Lookup("no-stream"))
	viper.BindPFlag("no_spinner", rootCmd.PersistentFlags().Lookup("no-spinner"))
	viper.BindPFlag("verbose_errors", rootCmd.PersistentFlags().Lookup("verbose-errors"))
	viper.BindPFlag("context.max_tool_output_lines", rootCmd.PersistentFlags().Lookup("max-tool-output"))
	viper.BindPFlag("context.max_tool_iterations", rootCmd.PersistentFlags().Lookup("max-iterations"))
	viper.BindPFlag("context.no_compaction", rootCmd.PersistentFlags().Lookup("no-compaction"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding home directory: %v\n", err)
			os.Exit(1)
		}

		configDir := home + "/.taracode"
		os.MkdirAll(configDir, 0755)

		viper.AddConfigPath(configDir)
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	// Set search configuration defaults
	viper.SetDefault("search.primary", "duckduckgo")
	viper.SetDefault("search.fallback", "searxng")
	viper.SetDefault("search.timeout", "10s")
	viper.SetDefault("search.retry_count", 1)
	viper.SetDefault("search.searxng_instance", "")

	// Set command streaming defaults
	viper.SetDefault("no_stream_commands", false)

	// Set context budget display defaults
	// max_context_tokens: Model's context window (32k default for Gemma3:27b)
	// show_context_budget: Whether to show token usage in prompt
	viper.SetDefault("max_context_tokens", 32768)
	viper.SetDefault("show_context_budget", true)

	// Edit preview mode defaults
	// preview_edits: Show diff preview before applying edit_file operations
	// preview_threshold: Minimum lines changed to trigger preview (0 = always preview)
	viper.SetDefault("preview_edits", true)
	viper.SetDefault("preview_threshold", 0)

	// Security configuration defaults
	// default_severity: Default severity filter for security scans (empty = all severities)
	// Common values: "HIGH,CRITICAL" or "MEDIUM,HIGH,CRITICAL"
	viper.SetDefault("security.default_severity", "")

	// MCP (Model Context Protocol) configuration defaults
	// enabled: Whether MCP support is enabled (default: true)
	// servers: List of MCP server configurations
	viper.SetDefault("mcp.enabled", true)

	// Memory (Persistent Project Knowledge) configuration defaults
	// enabled: Whether memory feature is enabled (default: true)
	// max_memories: Maximum number of memories per project (default: 500)
	// max_context_tokens: Maximum tokens to inject into prompt (default: 2000)
	// retention_days: Auto-cleanup memories not used in N days (default: 90)
	// auto_capture: Detect and suggest memories from conversation (default: true)
	viper.SetDefault("memory.enabled", true)
	viper.SetDefault("memory.max_memories", 500)
	viper.SetDefault("memory.max_context_tokens", 2000)
	viper.SetDefault("memory.retention_days", 90)
	viper.SetDefault("memory.auto_capture", true)

	// Context management defaults (v2.0.2)
	// max_tool_output_lines: Max lines per tool output (500 default, 0 = unlimited)
	// max_tool_output_chars: Max chars per tool output (15000 default, 0 = unlimited)
	// max_tool_iterations: Max tool call iterations per message (10 default)
	// compaction_enabled: Auto-compact conversation when context budget is high (default: true)
	// compaction_threshold: Trigger compaction at this fraction of max_context_tokens (default: 0.75)
	// compaction_keep_recent: Keep this many recent message pairs during compaction (default: 4)
	viper.SetDefault("context.max_tool_output_lines", 500)
	viper.SetDefault("context.max_tool_output_chars", 15000)
	viper.SetDefault("context.max_tool_iterations", 10)
	viper.SetDefault("context.compaction_enabled", true)
	viper.SetDefault("context.compaction_threshold", 0.75)
	viper.SetDefault("context.compaction_keep_recent", 4)

	// Model generation options (v2.0.4)
	// temperature: Sampling randomness, 0.0-2.0 (default: 0.7)
	// top_p: Nucleus sampling threshold, 0.0-1.0 (default: 0.9)
	// num_predict: Max tokens per response, 0 = model default (default: 0)
	viper.SetDefault("model.temperature", 0.7)
	viper.SetDefault("model.top_p", 0.9)
	viper.SetDefault("model.num_predict", 0)

	// Upgrade (Auto-update) configuration defaults
	// auto_check: Check for updates on startup (default: true)
	// auto_upgrade: Automatically install updates without prompting (default: false)
	// show_changelog: Show changelog when update is available (default: true)
	viper.SetDefault("upgrade.auto_check", true)
	viper.SetDefault("upgrade.auto_upgrade", false)
	viper.SetDefault("upgrade.show_changelog", true)

	viper.SetEnvPrefix("TARACODE")
	viper.AutomaticEnv()

	// Allow nested config via env vars (e.g., TARACODE_SEARCH_PRIMARY)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Silently read config - no need to announce for every command
	viper.ReadInConfig()
}
