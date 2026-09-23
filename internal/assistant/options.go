package assistant

import (
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
)

// Options is everything the assistant takes from configuration. cmd builds it from viper (config
// file, environment, flags) with the 2.x migrations; tests set fields directly. No internal package
// reads viper.
type Options struct {
	Host      string
	APIKey    string
	Model     string // "" = the persisted or first detected model
	Vendor    string // "" = auto-detect
	Streaming bool
	Spinner   bool

	WorkingDir string // "" = os.Getwd()
	Ephemeral  bool   // no storage: nothing is persisted, operate mode is unavailable

	Mode        policy.Mode // explicit (the --mode flag); wins over everything
	DefaultMode policy.Mode // from config; used when neither the flag nor a policy file sets one
	Offline     bool

	Generation       ModelOptions
	Think            string // auto|off|on|low|medium|high; parsed with llm.ParseThink
	KeepAlive        string
	ContextWindow    string // "auto" or a token count
	MaxContextTokens int    // compaction budget (max_context_tokens)
	Truncation       TruncationConfig
	Compaction       CompactionConfig
	MaxIterations    int

	PreviewEdits     bool
	PreviewThreshold int
	MemoryEnabled    bool
	MemoryMaxTokens  int

	Tools tools.Config
}

// DefaultOptions are the spec 5.9 defaults.
func DefaultOptions() Options {
	return Options{
		Streaming: true, Spinner: true,
		Generation:       ModelOptions{Temperature: 0.7, TopP: 0.9},
		Think:            "auto",
		ContextWindow:    "auto",
		MaxContextTokens: 32768,
		Truncation:       TruncationConfig{MaxLines: DefaultMaxToolOutputLines, MaxChars: DefaultMaxToolOutputChars},
		Compaction:       CompactionConfig{Enabled: true, Threshold: 0.75, KeepRecent: 4, MaxTokens: 32768},
		MaxIterations:    20,
		PreviewEdits:     true,
		MemoryEnabled:    true,
		MemoryMaxTokens:  2000,
	}
}
