package tools

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStreamingConfig(t *testing.T) {
	// Test enable/disable
	DisableStreaming()
	config := GetStreamingConfig()
	if config.Enabled {
		t.Error("Expected streaming to be disabled")
	}

	buf := &bytes.Buffer{}
	EnableStreaming(buf)
	config = GetStreamingConfig()
	if !config.Enabled {
		t.Error("Expected streaming to be enabled")
	}
	if config.Writer != buf {
		t.Error("Expected writer to be set")
	}

	DisableStreaming()
	config = GetStreamingConfig()
	if config.Enabled {
		t.Error("Expected streaming to be disabled after DisableStreaming")
	}
}

func TestExecuteCommandWithStreaming(t *testing.T) {
	buf := &bytes.Buffer{}
	EnableStreaming(buf)
	defer DisableStreaming()

	result, err := ExecuteCommand(map[string]interface{}{
		"command": "echo streaming test",
	}, "/tmp")

	if err != nil {
		t.Fatalf("ExecuteCommand with streaming failed: %v", err)
	}

	// Check that output was captured in result
	if !strings.Contains(result, "streaming test") {
		t.Errorf("Expected result to contain 'streaming test', got: %s", result)
	}

	// Give streaming writer time to flush
	time.Sleep(200 * time.Millisecond)

	// Check that output was also streamed to buffer
	streamed := buf.String()
	if !strings.Contains(streamed, "streaming test") {
		t.Errorf("Expected streamed output to contain 'streaming test', got: %s", streamed)
	}
}

func TestStreamingWriterFlush(t *testing.T) {
	capture := &bytes.Buffer{}
	target := &bytes.Buffer{}

	sw := newStreamingWriter(target, capture, 50*time.Millisecond, 10)

	// Write some data
	sw.Write([]byte("hello"))

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	// Stop before reading to avoid race condition
	sw.Stop()

	// Check target received data
	if !strings.Contains(target.String(), "hello") {
		t.Errorf("Expected target to contain 'hello', got: %s", target.String())
	}

	// Check capture also has data
	if !strings.Contains(capture.String(), "hello") {
		t.Errorf("Expected capture to contain 'hello', got: %s", capture.String())
	}
}

func TestStreamingWriterBufferSize(t *testing.T) {
	capture := &bytes.Buffer{}
	target := &bytes.Buffer{}

	// Small buffer size for immediate flush
	sw := newStreamingWriter(target, capture, 1*time.Second, 5)

	// Write data larger than buffer
	sw.Write([]byte("hello world"))

	// Should flush immediately due to buffer size
	time.Sleep(10 * time.Millisecond)

	// Stop before reading to avoid race condition
	sw.Stop()

	if !strings.Contains(target.String(), "hello world") {
		t.Errorf("Expected immediate flush, got: %s", target.String())
	}
}
