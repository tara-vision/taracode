package llm

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	openai "github.com/sashabaranov/go-openai"
)

// ErrStreamCut is matched by errors.Is when a streamed answer ends before the server's end marker.
var ErrStreamCut = errors.New("the model's server stopped mid-answer; `taracode doctor` shows why")

// ServerError is a refusal the model server explained: its HTTP status, its message and, when it gives one, a hint
// for the user. Model is the model the call was about, "" when none.
type ServerError struct {
	Status  int
	Message string
	Hint    string
	Model   string
}

// Error is the message, with the hint in parentheses when there is one.
func (e *ServerError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Hint)
	}
	return e.Message
}

// cutToolCall is Ollama's wording for a tool call that its output limit cut ("500: incomplete GLM tool call").
var cutToolCall = regexp.MustCompile(`incomplete [^ ]* ?tool call`)

// IsCutToolCall reports a server's refusal of a tool call that its output limit cut, which the server could then not
// parse: llama-server answers "Failed to parse tool call arguments as JSON ..." (through the engine's /v1, in the
// stream or as a 500), Ollama "500: incomplete GLM tool call". The refusal arrives as *ServerError from the engine's
// client and as go-openai's *openai.APIError from the generic OpenAI-compatible client, which is where Ollama runs.
// The agent sends the model back for a shorter call once per turn on it. Any other error, a cut stream included, is
// not one.
func IsCutToolCall(err error) bool {
	var refused *ServerError
	var apiErr *openai.APIError
	var msg string
	switch {
	case errors.As(err, &refused):
		msg = refused.Message
	case errors.As(err, &apiErr):
		msg = apiErr.Message
	default:
		return false
	}
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "failed to parse tool call arguments") || cutToolCall.MatchString(msg)
}

// UnreachableError is a server nothing answered at. Hint, when set, says how to start it.
type UnreachableError struct {
	Host string
	Hint string
	Err  error
}

// Error names the host, with the password of the URL's user info masked, and the hint when there is one.
func (e *UnreachableError) Error() string {
	msg := "nothing answers at " + RedactedHost(e.Host)
	if e.Hint != "" {
		msg += ": " + e.Hint
	}
	return msg
}

// Unwrap is the dial error underneath.
func (e *UnreachableError) Unwrap() error { return e.Err }

// RedactedHost is host with the password of its user info masked; a host that is not a URL is shown as given. A host
// reaches the screen through it, since the configuration can hold a user:password in the URL.
func RedactedHost(host string) string {
	if u, err := url.Parse(host); err == nil {
		return u.Redacted()
	}
	return host
}

// Printable is s without its control characters, tab and newline kept. Text a server sends reaches the terminal
// through it, so an escape sequence in that text cannot act on the terminal.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}
