package ollama

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// callSeq generates tool-call ids that are unique for the life of the process. Ollama's wire
// format carries no id of its own (fromWireToolCall invents one), so a counter that reset per call
// let two tool calls in one turn, across turns, or in a restored session share an id; toolNamesByID
// then resolved the shared id to whichever assistant message came last, mislabeling the other
// call's replayed tool result.
var callSeq atomic.Uint64

// wire types for Ollama's /api/chat.
type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

type toolCall struct {
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatRequest struct {
	Model     string          `json:"model"`
	Messages  []message       `json:"messages"`
	Tools     []openai.Tool   `json:"tools,omitempty"`
	Stream    bool            `json:"stream"`
	Think     any             `json:"think,omitempty"`
	Format    json.RawMessage `json:"format,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
	Options   map[string]any  `json:"options,omitempty"`
}

type chatChunk struct {
	Message         message `json:"message"`
	Done            bool    `json:"done"`
	DoneReason      string  `json:"done_reason"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	Error           string  `json:"error"`
}

// toWire converts a request into Ollama's JSON shape.
func toWire(req llm.Request, stream bool) chatRequest {
	names := toolNamesByID(req.Messages)
	out := chatRequest{
		Model:     req.Model,
		Stream:    stream,
		Tools:     req.Tools,
		KeepAlive: req.Options.KeepAlive,
		Format:    req.Options.Format,
	}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, toMessage(m, names))
	}
	switch req.Options.Think {
	case llm.ThinkAuto:
	case llm.ThinkOff:
		out.Think = false
	case llm.ThinkOn:
		out.Think = true
	default:
		out.Think = string(req.Options.Think)
	}
	opts := map[string]any{}
	if req.Options.NumCtx > 0 {
		opts["num_ctx"] = req.Options.NumCtx
	}
	if req.Options.Temperature != nil {
		// Widen to float64: json.Marshal formats a bare float32 with 32-bit shortest
		// round-trip precision, which does not equal float64(*Temperature) once decoded
		// back through JSON. float64 round-trips exactly.
		opts["temperature"] = float64(*req.Options.Temperature)
	}
	if req.Options.TopP != nil {
		opts["top_p"] = float64(*req.Options.TopP)
	}
	if req.Options.NumPredict > 0 {
		opts["num_predict"] = req.Options.NumPredict
	}
	if len(opts) > 0 {
		out.Options = opts
	}
	return out
}

// toolNamesByID maps assistant tool-call ids to function names so tool results can carry tool_name.
func toolNamesByID(msgs []openai.ChatCompletionMessage) map[string]string {
	names := map[string]string{}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			names[tc.ID] = tc.Function.Name
		}
	}
	return names
}

func toMessage(m openai.ChatCompletionMessage, names map[string]string) message {
	out := message{Role: m.Role, Content: m.Content}
	for _, part := range m.MultiContent {
		switch part.Type {
		case openai.ChatMessagePartTypeText:
			out.Content += part.Text
		case openai.ChatMessagePartTypeImageURL:
			if part.ImageURL != nil {
				out.Images = append(out.Images, stripDataURL(part.ImageURL.URL))
			}
		}
	}
	for _, tc := range m.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if !json.Valid(args) {
			args = json.RawMessage(`{}`)
		}
		out.ToolCalls = append(out.ToolCalls, toolCall{Function: toolFunction{Name: tc.Function.Name, Arguments: args}})
	}
	if m.Role == openai.ChatMessageRoleTool {
		out.ToolName = names[m.ToolCallID]
	}
	return out
}

// stripDataURL returns the base64 payload of a data: URL, or the input unchanged.
func stripDataURL(url string) string {
	if i := strings.Index(url, ";base64,"); strings.HasPrefix(url, "data:") && i >= 0 {
		return url[i+len(";base64,"):]
	}
	return url
}

// fromWireToolCall converts one Ollama tool call into the go-openai shape with a client-side id
// that is unique for the life of the process.
func fromWireToolCall(tc toolCall) openai.ToolCall {
	args := string(tc.Function.Arguments)
	if args == "" || args == "null" {
		args = "{}"
	}
	return openai.ToolCall{
		ID:       fmt.Sprintf("call_%d", callSeq.Add(1)),
		Type:     openai.ToolTypeFunction,
		Function: openai.FunctionCall{Name: tc.Function.Name, Arguments: args},
	}
}
