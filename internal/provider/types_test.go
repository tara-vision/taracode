package provider

import "testing"

func TestTypeNames(t *testing.T) {
	tests := []struct {
		typ          Type
		str, display string
	}{
		{TypeVLLM, "vllm", "vLLM"},
		{TypeOllama, "ollama", "Ollama"},
		{TypeLlamaCpp, "llama.cpp", "llama.cpp"},
		{TypeUnknown, "unknown", "Unknown"},
		{Type("lmstudio"), "lmstudio", "Unknown"},
	}
	for _, tt := range tests {
		if tt.typ.String() != tt.str || tt.typ.DisplayName() != tt.display {
			t.Errorf("%q: String %q, DisplayName %q", tt.typ, tt.typ.String(), tt.typ.DisplayName())
		}
	}
}

func TestFormatSizeIsInGigabytes(t *testing.T) {
	for size, want := range map[int64]string{0: "0.0GB", 3 << 29: "1.5GB", 17 << 30: "17.0GB"} {
		if got := (ModelInfo{Size: size}).FormatSize(); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", size, got, want)
		}
	}
}
