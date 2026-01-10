package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile   string
	host      string
	apiKey    string
	model     string
	vendor    string
	noStream  bool
	noSpinner bool
	Version   = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "taracode",
	Version: Version,
	Short: "Tara Code - AI-powered CLI assistant",
	Long: `Tara Code is a Claude Code-like CLI tool that provides an interactive
AI-powered assistant for software development tasks.`,
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
	rootCmd.PersistentFlags().BoolVar(&noStream, "no-stream", false, "disable streaming output (show response all at once)")
	rootCmd.PersistentFlags().BoolVar(&noSpinner, "no-spinner", false, "disable spinner animations")

	viper.BindPFlag("host", rootCmd.PersistentFlags().Lookup("host"))
	viper.BindPFlag("key", rootCmd.PersistentFlags().Lookup("key"))
	viper.BindPFlag("model", rootCmd.PersistentFlags().Lookup("model"))
	viper.BindPFlag("vendor", rootCmd.PersistentFlags().Lookup("vendor"))
	viper.BindPFlag("no_stream", rootCmd.PersistentFlags().Lookup("no-stream"))
	viper.BindPFlag("no_spinner", rootCmd.PersistentFlags().Lookup("no-spinner"))
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

	viper.SetEnvPrefix("TARACODE")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
