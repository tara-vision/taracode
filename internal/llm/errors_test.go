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
