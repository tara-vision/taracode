package assistant

import (
	"strings"
	"testing"
)

func TestServerContextAdvice(t *testing.T) {
	tests := []struct {
		name          string
		serverCtx     int
		systemTokens  int
		toolTokens    int
		configuredMax int
		threshold     float64
		wantWarn      bool
		wantContains  string
	}{
		{name: "unknown server context", serverCtx: 0, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: false},
		{name: "default 4096 overflows the tool budget", serverCtx: 4096, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: true, wantContains: "OLLAMA_CONTEXT_LENGTH"},
		{name: "server below the compaction point", serverCtx: 16384, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: true, wantContains: "max_context_tokens"},
		{name: "server slightly below config but above the compaction point", serverCtx: 32000, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: false},
		{name: "server matches config", serverCtx: 32000, systemTokens: 300, toolTokens: 5800, configuredMax: 32000, threshold: 0.75, wantWarn: false},
		{name: "server above config", serverCtx: 65536, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: false},
		{name: "threshold out of range compares to the full max", serverCtx: 30000, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0, wantWarn: true, wantContains: "max_context_tokens"},
		{name: "no configured max", serverCtx: 16384, systemTokens: 300, toolTokens: 5800, configuredMax: 0, threshold: 0.75, wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, warn := ServerContextAdvice(tt.serverCtx, tt.systemTokens, tt.toolTokens, tt.configuredMax, tt.threshold)
			if warn != tt.wantWarn {
				t.Fatalf("warn = %v, want %v (msg %q)", warn, tt.wantWarn, msg)
			}
			if tt.wantContains != "" && !strings.Contains(msg, tt.wantContains) {
				t.Fatalf("message %q does not contain %q", msg, tt.wantContains)
			}
			if !tt.wantWarn && msg != "" {
				t.Fatalf("expected empty message, got %q", msg)
			}
		})
	}
}
