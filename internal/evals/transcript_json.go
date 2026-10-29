package evals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// Transcript is one run of one task as JSON (--transcripts-json): the tools the model was offered,
// every message as sent with the reply's reasoning, the calls the gate decided, the score and the
// usage. Keys are snake_case, for a consumer outside the boards. Texts are scrubbed like the
// results (publicText, ruling P3-R45).
type Transcript struct {
	Task        string              `json:"task"`
	Area        string              `json:"area"`
	Mode        string              `json:"mode"`
	Model       string              `json:"model"`
	Run         int                 `json:"run"`
	Live        bool                `json:"live,omitempty"`
	Temperature float64             `json:"temperature"`
	Think       string              `json:"think"`
	Tools       []openai.Tool       `json:"tools"`
	Messages    []TranscriptMessage `json:"messages"`
	Calls       []TranscriptCall    `json:"calls"`
	Score       TranscriptScore     `json:"score"`
	Usage       TranscriptUsage     `json:"usage"`
	// Truncated, TimedOut and Error travel with the row, so a selector can drop a capped or broken run.
	Truncated bool   `json:"truncated,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Error     string `json:"error,omitempty"`
	// ReasoningAligned says every assistant message got its reply's reasoning; false means none was attached.
	ReasoningAligned bool `json:"reasoning_aligned"`
	// PrivateNames lists the lab's private names found anywhere in the transcript (ruling P3-R35): such a
	// transcript must not train a public model, and the selector drops it.
	PrivateNames []string `json:"private_names,omitempty"`
}

// TranscriptMessage is one message of the conversation; an assistant message carries the reasoning
// of the reply that produced it, which the conversation itself never stores.
type TranscriptMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Reasoning  string            `json:"reasoning,omitempty"`
	ToolCalls  []openai.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// TranscriptCall is one line of the calls block, structured: the decided call, its signature, the
// decision, the signatures the replay missed.
type TranscriptCall struct {
	Tool      string   `json:"tool"`
	Signature string   `json:"signature"`
	Decision  string   `json:"decision"` // allowed or denied
	Rule      string   `json:"rule,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Error     string   `json:"error,omitempty"`
	Missed    []string `json:"missed,omitempty"`
}

// TranscriptScore is the row's score.
type TranscriptScore struct {
	Total         float64  `json:"total"`
	Tools         float64  `json:"tools"`
	Answer        float64  `json:"answer"`
	Forbidden     float64  `json:"forbidden"`
	Pass          bool     `json:"pass"`
	SafetyFailure bool     `json:"safety_failure"`
	Notes         []string `json:"notes,omitempty"`
}

// TranscriptUsage is the row's token and time accounting.
type TranscriptUsage struct {
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
	Completions      int   `json:"completions"`
	WallMs           int64 `json:"wall_ms"`
}

// transcriptSource is what the transcript reads from the assistant after a turn.
type transcriptSource interface {
	Conversation() []openai.ChatCompletionMessage
	ToolDefinitions() []openai.Tool
}

// buildTranscript assembles one run's transcript from the conversation as sent, the replies observed
// in order, the calls the gate decided and the scored row. The i-th assistant message takes the i-th
// reply's reasoning.
func buildTranscript(
	t Task, opts RunOptions, run int, src transcriptSource, replies []llm.Result, calls []loggedCall, row TaskResult,
) Transcript {
	scrub := func(s string) string { return transcriptText(s, opts, t) }
	tr := Transcript{Task: t.ID, Area: string(t.Area), Mode: t.Mode, Model: opts.Model, Run: run, Live: opts.Live,
		Temperature: opts.Temperature, Think: opts.Think, Tools: src.ToolDefinitions(),
		Score: TranscriptScore{Total: row.Score, Tools: row.Tools, Answer: row.Answer, Forbidden: row.Forbidden,
			Pass: row.Pass, SafetyFailure: row.SafetyFailure, Notes: row.Notes},
		Usage: TranscriptUsage{PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens,
			Completions: row.Iterations, WallMs: row.WallMs},
		Truncated: row.Truncated, TimedOut: row.TimedOut, Error: row.Error}
	conversation := src.Conversation()
	assistants := 0
	for _, m := range conversation {
		if m.Role == openai.ChatMessageRoleAssistant {
			assistants++
		}
	}
	tr.ReasoningAligned = assistants == len(replies)
	assistant := 0
	for _, m := range conversation {
		msg := TranscriptMessage{Role: m.Role, Content: scrub(m.Content), ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			tc.Function.Arguments = scrub(tc.Function.Arguments)
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
		if m.Role == openai.ChatMessageRoleAssistant {
			if tr.ReasoningAligned {
				msg.Reasoning = scrub(replies[assistant].Thinking)
			}
			assistant++
		}
		tr.Messages = append(tr.Messages, msg)
	}
	for _, c := range calls {
		e := c.event
		call := TranscriptCall{Tool: e.Tool, Signature: scrub(Signature(e.Tool, e.Args)), Decision: "allowed"}
		for _, sig := range c.missedSignatures() {
			call.Missed = append(call.Missed, scrub(sig))
		}
		if !e.Allowed {
			call.Decision, call.Rule, call.Reason = "denied", e.Rule, scrub(e.Reason)
		}
		if e.Err != nil {
			call.Error = scrub(e.Err.Error())
		}
		tr.Calls = append(tr.Calls, call)
	}
	if tr.Calls == nil {
		tr.Calls = []TranscriptCall{}
	}
	if opts.scope != nil {
		tr.PrivateNames = privateNamesIn(tr, opts.scope.private)
	}
	return tr
}

// privateNamesIn lists the private names whose pattern matches anywhere in the transcript's JSON.
func privateNamesIn(tr Transcript, patterns []*regexp.Regexp) []string {
	if len(patterns) == 0 {
		return nil
	}
	data, err := json.Marshal(tr)
	if err != nil {
		return nil
	}
	var found []string
	for _, p := range patterns {
		if loc := p.FindStringIndex(string(data)); loc != nil {
			found = append(found, strings.Trim(string(data)[loc[0]:loc[1]], " \"\\:/,[]{}"))
		}
	}
	return found
}

// writeJSONTranscript writes the transcript next to the text log: <task>.json, or <task>.run<N>.json
// when the run repeats tasks.
func writeJSONTranscript(opts RunOptions, taskID string, run int, tr Transcript) error {
	name := taskID + ".json"
	if opts.Runs > 1 {
		name = fmt.Sprintf("%s.run%d.json", taskID, run)
	}
	dir := transcriptDir(opts)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // evals/runs is a git-ignored working directory
		return err
	}
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)                     // the corpus task id, validated by LoadTask
	return os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // a transcript under the runs directory
}
