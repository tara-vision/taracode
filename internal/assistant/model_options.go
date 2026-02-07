package assistant

import openai "github.com/sashabaranov/go-openai"

// Default model generation options
const (
	DefaultTemperature float32 = 0.7
	DefaultTopP        float32 = 0.9
	DefaultNumPredict  int     = 0 // 0 = model default (no limit)
)

// ModelOptions holds global model generation parameters.
// These are applied to main chat requests and serve as defaults for agents.
type ModelOptions struct {
	Temperature float32
	TopP        float32
	NumPredict  int
}

// ApplyTo applies model options to a chat completion request.
// Returns a modified copy - the original request is not mutated.
func (opts ModelOptions) ApplyTo(req openai.ChatCompletionRequest) openai.ChatCompletionRequest {
	if opts.Temperature != 0 {
		req.Temperature = opts.Temperature
	}
	if opts.TopP != 0 {
		req.TopP = opts.TopP
	}
	if opts.NumPredict > 0 {
		req.MaxTokens = opts.NumPredict
	}
	return req
}
