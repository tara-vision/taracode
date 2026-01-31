package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/mcp"
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
	rootCmd.PersistentFlags().StringVar(&host, "host", "", "LLM server URL (e.g., http://ollama.tara.lab)")
	rootCmd.PersistentFlags().StringVar(&apiKey, "key", "", "API key (optional for local servers)")
	rootCmd.PersistentFlags().StringVar(&model, "model", "", "model name (optional, auto-detected from server)")
	rootCmd.PersistentFlags().StringVar(&vendor, "vendor", "", "LLM vendor (auto, vllm, ollama, llama.cpp)")
	rootCmd.PersistentFlags().StringVar(&mode, "mode", "", "operating mode (devops, security)")
	rootCmd.PersistentFlags().StringVar(&severity, "severity", "", "default severity filter for security scans (e.g., HIGH,CRITICAL)")
	rootCmd.PersistentFlags().BoolVar(&noStream, "no-stream", false, "disable streaming output (show response all at once)")
	rootCmd.PersistentFlags().BoolVar(&noSpinner, "no-spinner", false, "disable spinner animations")
	rootCmd.PersistentFlags().BoolVar(&verboseErrors, "verbose-errors", false, "show detailed failure diagnostics with suggestions")

	viper.BindPFlag("host", rootCmd.PersistentFlags().Lookup("host"))
	viper.BindPFlag("key", rootCmd.PersistentFlags().Lookup("key"))
	viper.BindPFlag("model", rootCmd.PersistentFlags().Lookup("model"))
	viper.BindPFlag("vendor", rootCmd.PersistentFlags().Lookup("vendor"))
	viper.BindPFlag("mode", rootCmd.PersistentFlags().Lookup("mode"))
	viper.BindPFlag("security.default_severity", rootCmd.PersistentFlags().Lookup("severity"))
	viper.BindPFlag("no_stream", rootCmd.PersistentFlags().Lookup("no-stream"))
	viper.BindPFlag("no_spinner", rootCmd.PersistentFlags().Lookup("no-spinner"))
	viper.BindPFlag("verbose_errors", rootCmd.PersistentFlags().Lookup("verbose-errors"))
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

	viper.SetEnvPrefix("TARACODE")
	viper.AutomaticEnv()

	// Allow nested config via env vars (e.g., TARACODE_SEARCH_PRIMARY)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Silently read config - no need to announce for every command
	viper.ReadInConfig()
}
