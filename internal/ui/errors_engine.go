package ui

import (
	"errors"
	"net/http"

	"github.com/tara-vision/taracode/internal/llm"
)

// serverSuggestion is the suggestion for an error the model server explained (*llm.ServerError), a server nothing
// answers at (*llm.UnreachableError), or a stream cut mid-answer: the server's own hint first, since the engine
// writes its hints for exactly these cases, then taracode's advice.
func serverSuggestion(err error) (ErrorSuggestion, bool) {
	var refused *llm.ServerError
	var unreachable *llm.UnreachableError
	switch {
	case errors.As(err, &refused):
		title, advice := refusalAdvice(refused)
		return ErrorSuggestion{Title: title, Suggestions: withHint(refused.Hint, advice)}, true
	case errors.As(err, &unreachable):
		return ErrorSuggestion{Title: "Server Not Reachable", Suggestions: withHint(unreachable.Hint, []string{
			"Check host in ~/.taracode/config.yaml, or pass --host",
			"Run taracode doctor",
		})}, true
	case errors.Is(err, llm.ErrStreamCut):
		return ErrorSuggestion{Title: "Answer Cut Off", Suggestions: []string{
			"The model's server stopped before the answer was complete",
			"Run taracode doctor to see why",
		}}, true
	}
	return ErrorSuggestion{}, false
}

// refusalAdvice is the title and taracode's own suggestions for a refusal, by its status.
func refusalAdvice(e *llm.ServerError) (string, []string) {
	switch e.Status {
	case http.StatusUnauthorized:
		return "Engine Wants a Key", []string{"Set key in ~/.taracode/config.yaml, or pass --key"}
	case http.StatusNotFound:
		return "Model Not Pulled", []string{"Pull it: taraengine pull " + modelOr(e.Model), "Switch models with: /model"}
	case http.StatusConflict:
		return "Model Marked Faulty", []string{"Retry it: taraengine load " + modelOr(e.Model) + " --force",
			"taracode doctor shows the exit code and the last lines of the log"}
	case http.StatusBadGateway:
		return "Model Failed to Load", []string{"taracode doctor shows why"}
	case http.StatusServiceUnavailable:
		return "Engine Not Ready", []string{"taracode already retried once; check taraengine doctor if it persists"}
	case http.StatusGatewayTimeout:
		return "Model Load Timed Out", []string{"taracode doctor shows the model's state"}
	}
	return "Server Refused the Request", []string{"Run taracode doctor"}
}

// modelOr is the model a refusal names, or a placeholder when it names none.
func modelOr(model string) string {
	if model == "" {
		return "<model>"
	}
	return model
}

// withHint puts the server's hint in front of taracode's advice, when there is one.
func withHint(hint string, advice []string) []string {
	if hint == "" {
		return advice
	}
	return append([]string{hint}, advice...)
}

// StartupError formats an error from building the session: the server's own explanations get their suggestions,
// anything else is a connection problem.
func StartupError(host string, err error) string {
	if s, ok := serverSuggestion(err); ok {
		return FormatEnhancedError(err.Error(), s)
	}
	return FormatConnectionError(host, err)
}
