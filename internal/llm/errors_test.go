package llm

import (
	"errors"
	"fmt"
	"net"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestServerErrorReadsLikeTheEngine(t *testing.T) {
	plain := &ServerError{Status: 404, Message: "unknown model x"}
	hinted := &ServerError{Status: 409, Message: "model is faulty: x", Hint: "load it with force to try again"}
	if plain.Error() != "unknown model x" {
		t.Errorf("plain: %q", plain.Error())
	}
	if hinted.Error() != "model is faulty: x (load it with force to try again)" {
		t.Errorf("hinted: %q", hinted.Error())
	}
}

func TestUnreachableErrorNamesTheHostAndUnwraps(t *testing.T) {
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	bare := &UnreachableError{Host: "http://h:1", Err: dial}
	hinted := &UnreachableError{Host: "http://127.0.0.1:8272", Hint: "start taraengine with `taraengine serve`"}
	if bare.Error() != "nothing answers at http://h:1" || !errors.Is(bare, dial) {
		t.Errorf("bare: %q, unwraps to the dial error: %v", bare.Error(), errors.Is(bare, dial))
	}
	if hinted.Error() != "nothing answers at http://127.0.0.1:8272: start taraengine with `taraengine serve`" {
		t.Errorf("hinted: %q", hinted.Error())
	}
}

func TestErrStreamCutTellsWhereToLook(t *testing.T) {
	if ErrStreamCut.Error() != "the model's server stopped mid-answer; `taracode doctor` shows why" {
		t.Fatalf("ErrStreamCut = %q", ErrStreamCut.Error())
	}
}

func TestUnreachableErrorHidesThePasswordOfTheHost(t *testing.T) {
	withPassword := &UnreachableError{Host: "http://user:secret@proxy:8080", Hint: "start it"}
	if withPassword.Error() != "nothing answers at http://user:xxxxx@proxy:8080: start it" {
		t.Errorf("a host with a password: %q", withPassword.Error())
	}
	if notAURL := (&UnreachableError{Host: "http://bad host"}); notAURL.Error() != "nothing answers at http://bad host" {
		t.Errorf("a host that is not a URL is shown as given: %q", notAURL.Error())
	}
}

// TestRedactedHostMasksOnlyThePassword: whatever shows a host to the user shows it through RedactedHost, which keeps the
// user name, the port and the path, and shows a host that is not a URL as it was given.
func TestRedactedHostMasksOnlyThePassword(t *testing.T) {
	for host, want := range map[string]string{
		"http://user:secret@example.com:8272/v1": "http://user:xxxxx@example.com:8272/v1",
		"http://user@example.com:8272":           "http://user@example.com:8272",
		"http://127.0.0.1:8272/":                 "http://127.0.0.1:8272/",
		"http://bad host":                        "http://bad host",
		"127.0.0.1:8272":                         "127.0.0.1:8272",
		"":                                       "",
	} {
		if got := RedactedHost(host); got != want {
			t.Errorf("RedactedHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestPrintableDropsControlCharactersButTabAndNewline(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m\ttab\nline\r\x07bell\u009b2J\x00end"
	if got, want := Printable(in), "a[31mred[0m\ttab\nlinebell2Jend"; got != want {
		t.Fatalf("Printable(%q) = %q, want %q", in, got, want)
	}
	if got := Printable("plain text, ok"); got != "plain text, ok" {
		t.Fatalf("plain text changed: %q", got)
	}
}

// TestIsCutToolCallKnowsBothServersWording: the refusal of a tool call that the output limit cut is recognized in
// llama-server's wording (through the engine) and in Ollama's, in the engine's refusal and in go-openai's error (the
// generic path's), wrapped or not and whatever the case; a stream cut, a refusal about something else and a plain
// error are not one.
func TestIsCutToolCallKnowsBothServersWording(t *testing.T) {
	cut := []error{
		&ServerError{Status: 500, Message: "Failed to parse tool call arguments as JSON: syntax error at line 1, " +
			"column 10764: missing closing quote"},
		fmt.Errorf("chat: %w", &ServerError{Status: 500, Message: "500: incomplete GLM tool call"}),
		&ServerError{Status: 500, Message: "FAILED TO PARSE TOOL CALL ARGUMENTS as JSON"},
		fmt.Errorf("openai: %w", &openai.APIError{HTTPStatusCode: 500, Message: "500: incomplete GLM tool call"}),
		&openai.APIError{HTTPStatusCode: 500, Message: "Failed to parse tool call arguments as JSON: unexpected end"},
	}
	for _, err := range cut {
		if !IsCutToolCall(err) {
			t.Errorf("%v: not read as a cut tool call", err)
		}
	}
	whole := []error{
		&ServerError{Status: 404, Message: "unknown model: x"},
		&ServerError{Status: 500, Message: "catalog unreadable"},
		&ServerError{Status: 500, Message: "the tool call was incomplete"},
		ErrStreamCut,
		errors.New("failed to parse tool call arguments as JSON"),
		fmt.Errorf("openai: %w", &openai.APIError{HTTPStatusCode: 404, Message: "model not found"}),
		nil,
	}
	for _, err := range whole {
		if IsCutToolCall(err) {
			t.Errorf("%v: read as a cut tool call", err)
		}
	}
}
