package ui

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/tara-vision/taracode/internal/llm"
)

// cutToolCallAdvice is taracode's advice for a tool call that the output limit cut, on either path.
var cutToolCallAdvice = []string{"The model's tool call was longer than the output limit",
	"Raise generation.num_predict in config.yaml, or ask for a smaller step"}

// serverSuggestion is the suggestion for an error the model server explained (*llm.ServerError), a server nothing
// answers at (*llm.UnreachableError), a stream cut mid-answer, or a tool call the output limit cut as the generic
// OpenAI-compatible client reports it: the server's own hint first, since the engine writes its hints for exactly
// these cases, then taracode's advice.
func serverSuggestion(err error) (ErrorSuggestion, bool) {
	var refused *llm.ServerError
	var unreachable *llm.UnreachableError
	switch {
	case errors.As(err, &refused):
		title, advice := refusalAdvice(refused)
		return ErrorSuggestion{Title: title, Suggestions: withHint(refused.Hint, advice)}, true
	case llm.IsCutToolCall(err):
		return ErrorSuggestion{Title: "Tool Call Cut Off", Suggestions: cutToolCallAdvice}, true
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

// refusalAdvice is the title and taracode's own suggestions for a refusal: a tool call the output limit cut first,
// then by its status.
func refusalAdvice(e *llm.ServerError) (string, []string) {
	if llm.IsCutToolCall(e) {
		return "Tool Call Cut Off", cutToolCallAdvice
	}
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

// withoutHint is err's text without the server's hint, which the suggestions show first: the dimmed line under the
// title carries the error, and the hint shows once.
func withoutHint(err error) string {
	text := err.Error()
	var refused *llm.ServerError
	var unreachable *llm.UnreachableError
	switch {
	case errors.As(err, &refused) && refused.Hint != "":
		return strings.Replace(text, refused.Error(), refused.Message, 1)
	case errors.As(err, &unreachable) && unreachable.Hint != "":
		bare := &llm.UnreachableError{Host: unreachable.Host}
		return strings.Replace(text, unreachable.Error(), bare.Error(), 1)
	}
	return text
}

// StartupError formats an error from building the session. The server's own explanations get their suggestions, and
// so does an error the suggestion table knows that is not about the connection (an engine with nothing pulled, a
// model without tools); a connection failure, or an error nothing explains, gets the box that says taracode cannot
// connect.
func StartupError(host string, err error) string {
	if s, ok := serverSuggestion(err); ok {
		return FormatEnhancedError(withoutHint(err), s)
	}
	var netErr *net.OpError
	if !errors.As(err, &netErr) {
		if s, ok := matchSuggestion(err.Error(), false); ok {
			return FormatEnhancedError(err.Error(), s)
		}
	}
	return FormatConnectionError(host, err)
}
