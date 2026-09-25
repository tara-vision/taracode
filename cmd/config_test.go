package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/policy"
)

// resetConfig resets viper for a clean config test and re-establishes the defaults and the --model
// and --mode bindings: viper.Reset drops every prior BindPFlag call (including the ones root.go's
// init runs once at process start), so a test that wants the production shadowing path (ruling
// P2-R17) or the real --mode flag's Changed() state to reach viper.GetString("mode") needs them
// bound again.
func resetConfig(t *testing.T) {
	t.Helper()
	viper.Reset()
	setDefaults()
	_ = viper.BindPFlag("model", rootCmd.PersistentFlags().Lookup("model"))
	_ = viper.BindPFlag("mode", rootCmd.PersistentFlags().Lookup("mode"))
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

// TestLoadOptionsWarnsAboutTheRetiredHostsSection covers the 3.1.0 removal of the v2 multi-host
// pool: a config that still carries hosts: or default_host: starts on host: and says so once.
func TestLoadOptionsWarnsAboutTheRetiredHostsSection(t *testing.T) {
	resetConfig(t)
	viper.SetConfigType("yaml")
	retired := "host: http://localhost:11434\nhosts:\n  primary:\n    url: http://gpu:11434\ndefault_host: primary\n"
	if err := viper.ReadConfig(strings.NewReader(retired)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	_, warnings := loadOptions()
	if !strings.Contains(joined(warnings), "hosts: and default_host: are ignored since 3.1.0") {
		t.Fatalf("no hosts warning: %v", warnings)
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

// TestLoadOptionsModeFlagChangedWinsOverDefaultMode covers loadOptions' "if
// rootCmd.PersistentFlags().Changed('mode') { opts.Mode = opts.DefaultMode }" line through the real
// pflag rather than viper.Set: once --mode is actually changed on the command line, Mode (the
// explicit flag Options carries into applyStartupMode's precedence switch) and DefaultMode both
// resolve to the same value.
func TestLoadOptionsModeFlagChangedWinsOverDefaultMode(t *testing.T) {
	resetConfig(t)
	flag := rootCmd.PersistentFlags().Lookup("mode")
	if err := rootCmd.PersistentFlags().Set("mode", "operate"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = flag.Value.Set("")
		flag.Changed = false
	})

	opts, warnings := loadOptions()

	if len(warnings) != 0 {
		t.Fatalf("no warnings expected: %v", warnings)
	}
	if opts.Mode != policy.ModeOperate || opts.DefaultMode != policy.ModeOperate {
		t.Fatalf("Mode = %q DefaultMode = %q, want both operate once --mode is actually changed", opts.Mode, opts.DefaultMode)
	}
}

// TestLoadOptionsModeConfigDefaultWithoutTheFlagLeavesModeEmpty covers the other side: a mode:
// value that came from the config file, not the flag, only ever fills DefaultMode. Mode stays
// empty so applyStartupMode's precedence switch still lets a policy file's own mode win over it.
func TestLoadOptionsModeConfigDefaultWithoutTheFlagLeavesModeEmpty(t *testing.T) {
	resetConfig(t)
	viper.Set("mode", "operate")

	opts, _ := loadOptions()

	if opts.DefaultMode != policy.ModeOperate || opts.Mode != "" {
		t.Fatalf("DefaultMode = %q Mode = %q, want DefaultMode operate and Mode empty when the flag was not changed",
			opts.DefaultMode, opts.Mode)
	}
}
