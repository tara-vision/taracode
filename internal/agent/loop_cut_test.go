package agent

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
	"github.com/tara-vision/taracode/internal/policy"
)

// cutRefusal is llama-server's refusal, through the engine, of a tool call that the output limit cut.
var cutRefusal = taraenginetest.Turn{Status: http.StatusInternalServerError,
	Error: "Failed to parse tool call arguments as JSON: syntax error at line 1, column 10764: missing closing quote"}

// assistantOn is a test assistant on the fake engine through the engine's own client (vendor ""), or through the
// generic OpenAI-compatible client of the named vendor, with its output collected.
func assistantOn(t *testing.T, vendor string) (*Assistant, *taraenginetest.Server, *bytes.Buffer) {
	t.Helper()
	if vendor == "" {
		a, srv := newTestAssistant(t, false)
		var out bytes.Buffer
		a.out = &out
		return a, srv, &out
	}
	opts, srv, out := engineOptions(t)
	opts.Vendor = vendor
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	a.permissions = policy.AllowAll()
	return a, srv, out
}

// cutNudges counts the cut-call nudges in the assistant's conversation.
func cutNudges(a *Assistant) int {
	n := 0
	for _, m := range a.conversation {
		if m.Role == openai.ChatMessageRoleUser && m.Content == cutCallNudge {
			n++
		}
	}
	return n
}

// TestACutToolCallIsNudgedOncePerTurn: the server's refusal of a tool call that the output limit cut ends no turn by
// itself. The model is told once, as the next user message, the terminal says which limit cut it, and the model's
// next reply is the turn's answer. Ollama's wording arrives through the generic OpenAI-compatible client, as it does
// for an Ollama user, and is read the same way.
func TestACutToolCallIsNudgedOncePerTurn(t *testing.T) {
	ollama := taraenginetest.Turn{Status: http.StatusInternalServerError, Error: "500: incomplete GLM tool call"}
	cases := []struct {
		name       string
		cut        taraenginetest.Turn
		vendor     string // "" is the engine's own client; "ollama" the generic path
		numPredict int
		limit      string
	}{
		{"llama-server under the eval's cap", cutRefusal, "", 4096, "generation.num_predict 4096"},
		{"llama-server under the model's own limit", cutRefusal, "", 0, "the model's own output limit"},
		{"ollama on the generic path", ollama, "ollama", 0, "the model's own output limit"},
	}
	for _, c := range cases {
		a, srv, out := assistantOn(t, c.vendor)
		a.modelOptions.NumPredict = c.numPredict
		srv.Turns = []taraenginetest.Turn{c.cut, {Content: "The secret is in .env."}}
		if err := a.ProcessMessage("sweep the repository for secrets"); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if a.GetLastResponse() != "The secret is in .env." || countPath(srv, chatPath) != 2 || cutNudges(a) != 1 {
			t.Fatalf("%s: last response %q, chat requests %d, nudges %d", c.name, a.GetLastResponse(),
				countPath(srv, chatPath), cutNudges(a))
		}
		want := "The model's tool call was cut at the output limit (" + c.limit + "); asking it to call again with " +
			"shorter arguments"
		if strings.Count(out.String(), want) != 1 {
			t.Fatalf("%s: output %q lacks %q once", c.name, out.String(), want)
		}
	}
}

// TestASecondCutToolCallEndsTheTurn: a second cut in the same turn is the server's error, as any other refusal; the
// model was nudged once, and the next turn may nudge again.
func TestASecondCutToolCallEndsTheTurn(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.out = &bytes.Buffer{}
	srv.Turns = []taraenginetest.Turn{cutRefusal, cutRefusal, cutRefusal, {Content: "Done."}}
	err := a.ProcessMessage("sweep")
	var refused *llm.ServerError
	if !errors.As(err, &refused) || !llm.IsCutToolCall(err) || countPath(srv, chatPath) != 2 || cutNudges(a) != 1 {
		t.Fatalf("err %#v, chat requests %d, nudges %d", err, countPath(srv, chatPath), cutNudges(a))
	}
	if err := a.ProcessMessage("try again"); err != nil || a.GetLastResponse() != "Done." || cutNudges(a) != 2 {
		t.Fatalf("the next turn: %v, %q, nudges %d", err, a.GetLastResponse(), cutNudges(a))
	}
}
