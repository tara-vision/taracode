package llm

import (
	"errors"
	"net"
	"testing"
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

func TestPrintableDropsControlCharactersButTabAndNewline(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m\ttab\nline\r\x07bell\u009b2J\x00end"
	if got, want := Printable(in), "a[31mred[0m\ttab\nlinebell2Jend"; got != want {
		t.Fatalf("Printable(%q) = %q, want %q", in, got, want)
	}
	if got := Printable("plain text, ok"); got != "plain text, ok" {
		t.Fatalf("plain text changed: %q", got)
	}
}
