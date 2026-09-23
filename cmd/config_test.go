package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// resetConfig resets viper for a clean config test and re-establishes the defaults and the --model
// binding: viper.Reset drops every prior BindPFlag call (including the one root.go's init runs),
// so a test that wants the production shadowing path (ruling P2-R17) needs it back.
func resetConfig(t *testing.T) {
	t.Helper()
	viper.Reset()
	setDefaults()
	_ = viper.BindPFlag("model", rootCmd.PersistentFlags().Lookup("model"))
	t.Cleanup(viper.Reset)
}

func joined(warnings []string) string { return strings.Join(warnings, "\n") }

func TestLoadOptionsDefaults(t *testing.T) {
	resetConfig(t)
	opts, warnings := loadOptions()
	if len(warnings) != 0 {
		t.Fatalf("no warnings expected: %v", warnings)
	}
	if opts.MaxIterations != 20 || opts.ContextWindow != "auto" || opts.Generation.Temperature != 0.7 || opts.Generation.TopP != 0.9 ||
		opts.Compaction.Threshold != 0.75 || opts.Compaction.KeepRecent != 4 || !opts.PreviewEdits || !opts.MemoryEnabled || opts.MemoryMaxTokens != 2000 ||
		opts.Truncation.MaxLines != 500 || opts.Truncation.MaxChars != 15000 || opts.Offline || opts.Mode != "" || opts.DefaultMode != "" {
		t.Fatalf("%+v", opts)
	}
}

// TestLoadOptionsMigratesTheTwoPointXLayout covers the 2.x compatibility path: model: as a
// section, security.default_severity, the retired agents: and watch: sections, and a 2.x mode
// name. It loads the 2.x YAML through the config layer (not viper.Set) with the --model pflag
// bound, so it exercises the same nested-key shadowing viper 1.21 applies in the real binary once
// --model is bound to the "model" key (ruling P2-R17) - viper.Set would instead land in the
// override map, which is searched before the shadowing check and would hide the defect.
func TestLoadOptionsMigratesTheTwoPointXLayout(t *testing.T) {
	resetConfig(t)
	viper.SetConfigType("yaml")
	twoPointX := `
model:
  temperature: 0.3
  top_p: 0.5
  num_predict: 512
security:
  default_severity: "HIGH,CRITICAL"
agents:
  enabled: true
watch:
  interval: 5
mode: security
`
	if err := viper.ReadConfig(strings.NewReader(twoPointX)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	opts, warnings := loadOptions()
	if opts.Model != "" || opts.Generation.Temperature != 0.3 || opts.Generation.TopP != 0.5 || opts.Generation.NumPredict != 512 {
		t.Fatalf("generation from the 2.x model section: %+v", opts.Generation)
	}
	if opts.Tools.DefaultSeverity != "HIGH,CRITICAL" {
		t.Fatalf("severity %q", opts.Tools.DefaultSeverity)
	}
	if opts.DefaultMode != "" || opts.Mode != "" {
		t.Fatalf("2.x modes map to no mode: %+v", opts)
	}
	for _, want := range []string{"model:", "generation:", "security.default_severity", "scan.default_severity", "agents:", "watch:", "mode \"security\""} {
		if !strings.Contains(joined(warnings), want) {
			t.Errorf("warnings lack %q:\n%s", want, joined(warnings))
		}
	}
}

func TestLoadOptionsReadsTheV3Keys(t *testing.T) {
	resetConfig(t)
	viper.Set("model", "qwen-test")
	viper.Set("generation.temperature", 0.1)
	viper.Set("mode", "operate")
	viper.Set("offline", true)
	viper.Set("scan.default_severity", "LOW")
	viper.Set("context.max_tool_iterations", 7)
	viper.Set("think", "high")
	opts, warnings := loadOptions()
	if len(warnings) != 0 {
		t.Fatalf("%v", warnings)
	}
	if opts.Model != "qwen-test" || opts.Generation.Temperature != 0.1 || opts.DefaultMode != "operate" || !opts.Offline ||
		opts.Tools.DefaultSeverity != "LOW" || opts.MaxIterations != 7 || opts.Think != "high" {
		t.Fatalf("%+v", opts)
	}
	viper.Set("mode", "yolo")
	if _, warnings := loadOptions(); !strings.Contains(joined(warnings), "yolo") {
		t.Error("an unknown mode warns")
	}
}
