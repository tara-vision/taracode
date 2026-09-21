package assistant

import "testing"

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

func TestModelOptions_LLMValues_AllValues(t *testing.T) {
	opts := ModelOptions{Temperature: 0.5, TopP: 0.8, NumPredict: 512}

	temperature, topP, numPredict := opts.LLMValues()

	if temperature == nil || *temperature != 0.5 {
		t.Errorf("temperature = %v, want 0.5", temperature)
	}
	if topP == nil || *topP != 0.8 {
		t.Errorf("topP = %v, want 0.8", topP)
	}
	if numPredict != 512 {
		t.Errorf("numPredict = %d, want 512", numPredict)
	}
}

func TestModelOptions_LLMValues_UnsetStayNil(t *testing.T) {
	temperature, topP, numPredict := ModelOptions{}.LLMValues()

	if temperature != nil {
		t.Errorf("temperature = %v, want nil", *temperature)
	}
	if topP != nil {
		t.Errorf("topP = %v, want nil", *topP)
	}
	if numPredict != 0 {
		t.Errorf("numPredict = %d, want 0", numPredict)
	}
}

// TestModelOptions_LLMValues_Immutability guards against LLMValues handing back a pointer into
// the receiver itself: the returned pointers must point at fresh copies, so mutating what they
// point to can never reach back into the ModelOptions the caller holds.
func TestModelOptions_LLMValues_Immutability(t *testing.T) {
	opts := ModelOptions{Temperature: 0.5, TopP: 0.8, NumPredict: 512}

	temperature, topP, _ := opts.LLMValues()
	*temperature = 99
	*topP = 99

	if opts.Temperature != 0.5 {
		t.Errorf("opts.Temperature = %f, want unchanged 0.5 after mutating the returned pointer", opts.Temperature)
	}
	if opts.TopP != 0.8 {
		t.Errorf("opts.TopP = %f, want unchanged 0.8 after mutating the returned pointer", opts.TopP)
	}
}
