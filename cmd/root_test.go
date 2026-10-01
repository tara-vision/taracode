package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestGetSearchConfig(t *testing.T) {
	resetConfig(t)
	viper.Set("search.primary", "searxng")
	viper.Set("search.fallback", "duckduckgo")
	viper.Set("search.timeout", "3s")
	viper.Set("search.retry_count", 2)
	viper.Set("search.searxng_instance", "https://searx.example")
	viper.Set("brave_api_key", "top-level-key")
	got := GetSearchConfig()
	want := SearchConfig{Primary: "searxng", Fallback: "duckduckgo", Timeout: "3s", RetryCount: 2,
		SearXNGInstance: "https://searx.example", BraveAPIKey: "top-level-key"}
	if got != want {
		t.Fatalf("GetSearchConfig() = %+v, want %+v", got, want)
	}
	viper.Set("search.brave_api_key", "nested-key")
	if got := GetSearchConfig().BraveAPIKey; got != "nested-key" {
		t.Fatalf("the nested key wins over the top-level one: %q", got)
	}
}

func TestGetSecurityConfig(t *testing.T) {
	resetConfig(t)
	viper.Set("security.default_severity", "HIGH")
	if got := GetSecurityConfig(); got.DefaultSeverity != "HIGH" {
		t.Fatalf("GetSecurityConfig() = %+v", got)
	}
}

const mcpConfigYAML = `
mcp:
  enabled: true
  servers:
    - name: github
      command: npx
      args: ["-y", "server-github", 7]
      env:
        token: "abc"
        retries: 3
      auto_connect: true
      timeout: 15s
    - name: slow
      command: slow-server
      timeout: forever
    - name: no-command
    - command: no-name
`

func TestGetMCPConfigReadsTheServers(t *testing.T) {
	resetConfig(t)
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(mcpConfigYAML)); err != nil {
		t.Fatal(err)
	}
	cfg := GetMCPConfig()
	if !cfg.Enabled || len(cfg.Servers) != 2 {
		t.Fatalf("config %+v: the entries without a name or a command are left out", cfg)
	}
	github, slow := cfg.Servers[0], cfg.Servers[1]
	// The string entries come through as written.
	if github.Name != "github" || github.Command != "npx" || len(github.Args) < 2 || github.Args[0] != "-y" ||
		github.Args[1] != "server-github" || github.Env["token"] != "abc" || !github.AutoConnect ||
		github.Timeout != 15*time.Second {
		t.Fatalf("github %+v", github)
	}
	if slow.Name != "slow" || slow.Timeout != 0 || slow.AutoConnect || slow.Args != nil || slow.Env != nil {
		t.Fatalf("slow %+v: a timeout that does not parse is left unset", slow)
	}

	resetConfig(t)
	viper.Set("mcp.servers", "not a list")
	if cfg := GetMCPConfig(); len(cfg.Servers) != 0 {
		t.Fatalf("servers that do not parse: %+v", cfg)
	}
}

func TestIsValidSeverity(t *testing.T) {
	tests := []struct {
		severity string
		want     bool
	}{
		{"", true},
		{"HIGH", true},
		{"high, critical", true},
		{"UNKNOWN,LOW,MEDIUM", true},
		{"HIGH,SEVERE", false},
		{"HIGH,", false},
	}
	for _, tt := range tests {
		if got := IsValidSeverity(tt.severity); got != tt.want {
			t.Errorf("IsValidSeverity(%q) = %v, want %v", tt.severity, got, tt.want)
		}
	}
}

func TestRootRejectsAnInvalidSeverity(t *testing.T) {
	resetConfig(t)
	if err := rootCmd.PreRunE(rootCmd, nil); err != nil {
		t.Fatalf("no severity: %v", err)
	}
	viper.Set("scan.default_severity", "HIGH,SEVERE")
	err := rootCmd.PreRunE(rootCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid severity filter: HIGH,SEVERE") ||
		!strings.Contains(err.Error(), "UNKNOWN, LOW, MEDIUM, HIGH, CRITICAL") {
		t.Fatalf("err = %v", err)
	}
}

// executeWith runs Execute with args and returns what the command printed. initConfig turns on
// viper's TARACODE_* environment lookup, so any such variable of the shell running the tests is
// blanked first (viper ignores an empty one).
func executeWith(t *testing.T, args ...string) (string, error) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "TARACODE_") {
			t.Setenv(name, "")
		}
	}
	var out bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	err := Execute()
	return out.String(), err
}

// TestExecuteReadsTheConfigFile runs a subcommand through Execute with --config: initConfig reads
// that file instead of ~/.taracode/config.yaml.
func TestExecuteReadsTheConfigFile(t *testing.T) {
	resetConfig(t)
	isolateHome(t)
	config := filepath.Join(t.TempDir(), "taracode.yaml")
	writeFile(t, config, "search:\n  primary: searxng\n")
	t.Cleanup(func() {
		cfgFile = ""
		rootCmd.PersistentFlags().Lookup("config").Changed = false
	})
	out, err := executeWith(t, "--config", config, "eval", "lint", "--corpus", t.TempDir())
	if err != nil || !strings.Contains(out, "ok: 0 task(s)") {
		t.Fatalf("out %q err %v", out, err)
	}
	if got := viper.GetString("search.primary"); got != "searxng" {
		t.Fatalf("search.primary = %q, want the config file's value", got)
	}
}

// TestExecuteCreatesTheConfigDirectory: without --config, initConfig looks in ~/.taracode and
// creates it.
func TestExecuteCreatesTheConfigDirectory(t *testing.T) {
	resetConfig(t)
	home := isolateHome(t)
	if _, err := executeWith(t, "eval", "lint", "--corpus", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(home, ".taracode")); err != nil || !info.IsDir() {
		t.Fatalf("~/.taracode: %v", err)
	}
}
