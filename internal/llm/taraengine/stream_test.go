package taraengine

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/tara-vision/taracode/internal/llm"
)

func collect(events *[]llm.Event) func(llm.Event) error {
	return func(e llm.Event) error { *events = append(*events, e); return nil }
}

func TestReadStreamSkipsCommentsAndBlankLines(t *testing.T) {
	stream := ": keep-alive\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	var events []llm.Event
	res, err := readStream(strings.NewReader(stream), collect(&events))
	if err != nil || res.Content != "hi" || len(events) != 2 || events[1].Kind != llm.EventUsage {
		t.Fatalf("result %+v, events %+v, err %v", res, events, err)
	}
}

func TestReadStreamReportsBadAndErrorChunks(t *testing.T) {
	for stream, want := range map[string]string{
		"data: {not json}\n\n":                         "decode a stream chunk",
		"data: {\"error\":{\"message\":\"boom\"}}\n\n": "taraengine: boom",
	} {
		if _, err := readStream(strings.NewReader(stream), collect(new([]llm.Event))); err == nil ||
			!strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", stream, err)
		}
	}
}

func TestReadStreamGivesToolCallsAnIDAndAType(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"noop\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	res, err := readStream(strings.NewReader(stream), collect(new([]llm.Event)))
	if err != nil || len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call_0" || res.ToolCalls[0].Type != "function" ||
		res.ToolCalls[0].Index != nil {
		t.Fatalf("result %+v %v", res, err)
	}
}

func TestReadStreamMergesFragmentsByIndex(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"{\\\"a\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_x\",\"function\":{\"name\":\"f\"," +
		"\"arguments\":\"\\\":1}\"}}]}}]}\n\ndata: [DONE]\n\n"
	res, err := readStream(strings.NewReader(stream), collect(new([]llm.Event)))
	if err != nil || len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call_x" || res.ToolCalls[0].Function.Name != "f" ||
		res.ToolCalls[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("result %+v %v", res, err)
	}
}

func TestReadStreamTakesALongLine(t *testing.T) {
	long := strings.Repeat("x", 100<<10)
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"" + long + "\"}}]}\n\ndata: [DONE]\n\n"
	if res, err := readStream(strings.NewReader(stream), collect(new([]llm.Event))); err != nil || len(res.Content) != len(long) {
		t.Fatalf("a 100 KiB delta: %v", err)
	}
}

func TestReadStreamReportsACutAndAReadError(t *testing.T) {
	if _, err := readStream(strings.NewReader("data: {\"choices\":[]}\n\n"), collect(new([]llm.Event))); !errors.Is(err,
		llm.ErrStreamCut) {
		t.Fatalf("no [DONE]: %v", err)
	}
	broken := io.MultiReader(strings.NewReader("data: {\"choices\":[]}\n\n"), iotest.ErrReader(io.ErrUnexpectedEOF))
	if _, err := readStream(broken, collect(new([]llm.Event))); !errors.Is(err, llm.ErrStreamCut) ||
		!errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a read error: %v", err)
	}
}

func TestReadStreamReadsAMidLineCutAsACut(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"choi"
	var events []llm.Event
	_, err := readStream(strings.NewReader(stream), collect(&events))
	if !errors.Is(err, llm.ErrStreamCut) || len(events) != 1 || events[0].Text != "hi" {
		t.Fatalf("a stream cut mid-line: %v, events %+v", err, events)
	}
}

func TestScanTerminatedLinesDropsOnlyAnUnfinishedLastLine(t *testing.T) {
	if advance, token, err := scanTerminatedLines([]byte("data: a\nda"), true); advance != 8 || string(token) != "data: a" || err != nil {
		t.Errorf("a terminated line before an unfinished one: %d %q %v", advance, token, err)
	}
	if advance, token, _ := scanTerminatedLines([]byte("data: a"), false); advance != 0 || token != nil {
		t.Errorf("an unfinished line before the end of input: %d %q", advance, token)
	}
	if advance, token, _ := scanTerminatedLines([]byte("data: a"), true); advance != 7 || token != nil {
		t.Errorf("an unfinished last line: %d %q", advance, token)
	}
	if advance, token, _ := scanTerminatedLines(nil, true); advance != 0 || token != nil {
		t.Errorf("no input left: %d %q", advance, token)
	}
}

func TestReadStreamStopsWhenOnEventFails(t *testing.T) {
	stop := errors.New("stop")
	streams := map[llm.EventKind]string{
		llm.EventThinking: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"x\"}}]}\n\n",
		llm.EventText:     "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n",
		llm.EventToolCall: "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"f\"}}]}}]}\n\ndata: [DONE]\n\n",
		llm.EventUsage:    "data: [DONE]\n\n",
	}
	for kind, stream := range streams {
		failOn := func(e llm.Event) error {
			if e.Kind == kind {
				return stop
			}
			return nil
		}
		if _, err := readStream(strings.NewReader(stream), failOn); !errors.Is(err, stop) {
			t.Errorf("kind %d: %v", kind, err)
		}
	}
}
