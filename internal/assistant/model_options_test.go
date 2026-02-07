package assistant

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestModelOptions_ApplyTo_AllValues(t *testing.T) {
	opts := ModelOptions{
		Temperature: 0.5,
		TopP:        0.8,
		NumPredict:  512,
	}

	req := openai.ChatCompletionRequest{
		Model: "test-model",
	}

	result := opts.ApplyTo(req)

	if result.Temperature != 0.5 {
		t.Errorf("Temperature = %f, want 0.5", result.Temperature)
	}
	if result.TopP != 0.8 {
		t.Errorf("TopP = %f, want 0.8", result.TopP)
	}
	if result.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d, want 512", result.MaxTokens)
	}
}

func TestModelOptions_ApplyTo_ZeroValuesNotApplied(t *testing.T) {
	opts := ModelOptions{
		Temperature: 0,
		TopP:        0,
		NumPredict:  0,
	}

	req := openai.ChatCompletionRequest{
		Model:       "test-model",
		Temperature: 0.9,
		TopP:        0.95,
		MaxTokens:   1024,
	}

	result := opts.ApplyTo(req)

	if result.Temperature != 0.9 {
		t.Errorf("Temperature = %f, want 0.9 (should not be overwritten by zero)", result.Temperature)
	}
	if result.TopP != 0.95 {
		t.Errorf("TopP = %f, want 0.95 (should not be overwritten by zero)", result.TopP)
	}
	if result.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024 (should not be overwritten by zero)", result.MaxTokens)
	}
}

func TestModelOptions_ApplyTo_Immutability(t *testing.T) {
	opts := ModelOptions{
		Temperature: 0.3,
		TopP:        0.7,
		NumPredict:  256,
	}

	original := openai.ChatCompletionRequest{
		Model:       "test-model",
		Temperature: 0.9,
		TopP:        0.95,
		MaxTokens:   1024,
	}

	result := opts.ApplyTo(original)

	// Original must not be modified
	if original.Temperature != 0.9 {
		t.Errorf("original.Temperature = %f, want 0.9 (must not be mutated)", original.Temperature)
	}
	if original.TopP != 0.95 {
		t.Errorf("original.TopP = %f, want 0.95 (must not be mutated)", original.TopP)
	}
	if original.MaxTokens != 1024 {
		t.Errorf("original.MaxTokens = %d, want 1024 (must not be mutated)", original.MaxTokens)
	}

	// Result must have new values
	if result.Temperature != 0.3 {
		t.Errorf("result.Temperature = %f, want 0.3", result.Temperature)
	}
	if result.TopP != 0.7 {
		t.Errorf("result.TopP = %f, want 0.7", result.TopP)
	}
	if result.MaxTokens != 256 {
		t.Errorf("result.MaxTokens = %d, want 256", result.MaxTokens)
	}
}

func TestModelOptions_ApplyTo_PreservesOtherFields(t *testing.T) {
	opts := ModelOptions{
		Temperature: 0.5,
	}

	tools := []openai.Tool{
		{Type: openai.ToolTypeFunction},
	}
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: "hello"},
	}

	req := openai.ChatCompletionRequest{
		Model:    "test-model",
		Messages: messages,
		Tools:    tools,
	}

	result := opts.ApplyTo(req)

	if result.Model != "test-model" {
		t.Errorf("Model = %s, want test-model", result.Model)
	}
	if len(result.Messages) != 1 {
		t.Errorf("Messages length = %d, want 1", len(result.Messages))
	}
	if len(result.Tools) != 1 {
		t.Errorf("Tools length = %d, want 1", len(result.Tools))
	}
}

func TestModelOptions_Defaults(t *testing.T) {
	if DefaultTemperature != 0.7 {
		t.Errorf("DefaultTemperature = %f, want 0.7", DefaultTemperature)
	}
	if DefaultTopP != 0.9 {
		t.Errorf("DefaultTopP = %f, want 0.9", DefaultTopP)
	}
	if DefaultNumPredict != 0 {
		t.Errorf("DefaultNumPredict = %d, want 0", DefaultNumPredict)
	}
}
