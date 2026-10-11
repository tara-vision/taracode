package llm

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
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

// UnreachableError is a server nothing answered at. Hint, when set, says how to start it.
type UnreachableError struct {
	Host string
	Hint string
	Err  error
}

// Error names the host, with the password of the URL's user info masked, and the hint when there is one.
func (e *UnreachableError) Error() string {
	msg := "nothing answers at " + redacted(e.Host)
	if e.Hint != "" {
		msg += ": " + e.Hint
	}
	return msg
}

// Unwrap is the dial error underneath.
func (e *UnreachableError) Unwrap() error { return e.Err }

// redacted is host with the password of its user info masked; a host that is not a URL is shown as given.
func redacted(host string) string {
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
