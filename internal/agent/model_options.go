package agent

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

// LLMValues returns the options in the shape llm.Options wants: nil pointers for temperature and
// top_p when they are unset, so the request leaves them out and the server keeps its own default.
func (opts ModelOptions) LLMValues() (temperature, topP *float32, numPredict int) {
	if opts.Temperature != 0 {
		value := opts.Temperature
		temperature = &value
	}
	if opts.TopP != 0 {
		value := opts.TopP
		topP = &value
	}
	if opts.NumPredict > 0 {
		numPredict = opts.NumPredict
	}
	return temperature, topP, numPredict
}
