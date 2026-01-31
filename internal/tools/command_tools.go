package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const defaultCommandTimeout = 60 * time.Second

// StreamingConfig holds configuration for streaming command output
type StreamingConfig struct {
	Enabled       bool          // Whether streaming is enabled
	Writer        io.Writer     // Writer for streaming output (e.g., os.Stdout)
	FlushInterval time.Duration // How often to flush buffered output
	BufferSize    int           // Size of the output buffer before flushing
}

var (
	streamingConfig StreamingConfig
	streamingMu     sync.RWMutex
)

// SetStreamingConfig configures command output streaming
func SetStreamingConfig(config StreamingConfig) {
	streamingMu.Lock()
	defer streamingMu.Unlock()
	streamingConfig = config
}

// GetStreamingConfig returns the current streaming configuration
func GetStreamingConfig() StreamingConfig {
	streamingMu.RLock()
	defer streamingMu.RUnlock()
	return streamingConfig
}

// EnableStreaming is a convenience function to enable streaming to a writer
func EnableStreaming(w io.Writer) {
	SetStreamingConfig(StreamingConfig{
		Enabled:       true,
		Writer:        w,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    256,
	})
}

// DisableStreaming turns off command output streaming
func DisableStreaming() {
	SetStreamingConfig(StreamingConfig{Enabled: false})
}

// streamingWriter wraps an io.Writer to provide buffered streaming with flush control
type streamingWriter struct {
	target        io.Writer
	buffer        *bytes.Buffer
	capture       *bytes.Buffer // Captures all output for final result
	mu            sync.Mutex
	flushInterval time.Duration
	bufferSize    int
	stopChan      chan struct{}
	doneChan      chan struct{}
}

func newStreamingWriter(target io.Writer, capture *bytes.Buffer, flushInterval time.Duration, bufferSize int) *streamingWriter {
	sw := &streamingWriter{
		target:        target,
		buffer:        &bytes.Buffer{},
		capture:       capture,
		flushInterval: flushInterval,
		bufferSize:    bufferSize,
		stopChan:      make(chan struct{}),
		doneChan:      make(chan struct{}),
	}

	// Start background flusher
	go sw.flushLoop()

	return sw
}

func (sw *streamingWriter) Write(p []byte) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	// Always capture for final result
	sw.capture.Write(p)

	// Buffer for streaming
	sw.buffer.Write(p)

	// Flush if buffer is large enough
	if sw.buffer.Len() >= sw.bufferSize {
		sw.flushLocked()
	}

	return len(p), nil
}

func (sw *streamingWriter) flushLoop() {
	ticker := time.NewTicker(sw.flushInterval)
	defer ticker.Stop()
	defer close(sw.doneChan)

	for {
		select {
		case <-sw.stopChan:
			// Final flush
			sw.mu.Lock()
			sw.flushLocked()
			sw.mu.Unlock()
			return
		case <-ticker.C:
			sw.mu.Lock()
			sw.flushLocked()
			sw.mu.Unlock()
		}
	}
}

func (sw *streamingWriter) flushLocked() {
	if sw.buffer.Len() > 0 && sw.target != nil {
		sw.target.Write(sw.buffer.Bytes())
		sw.buffer.Reset()
	}
}

func (sw *streamingWriter) Stop() {
	close(sw.stopChan)
	<-sw.doneChan
}

func ExecuteCommand(params map[string]interface{}, workingDir string) (string, error) {
	command, ok := params["command"].(string)
	if !ok {
		return "", fmt.Errorf("command parameter is required")
	}

	// Get optional timeout from params (in seconds)
	timeout := defaultCommandTimeout
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	// Check for streaming configuration
	config := GetStreamingConfig()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create command with context
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workingDir

	// Capture output - use streaming if enabled
	var stdout, stderr bytes.Buffer
	var stdoutWriter, stderrWriter io.Writer
	var streamWriter *streamingWriter

	if config.Enabled && config.Writer != nil {
		// Create streaming writer that writes to both the capture buffer and live output
		streamWriter = newStreamingWriter(
			config.Writer,
			&stdout,
			config.FlushInterval,
			config.BufferSize,
		)
		stdoutWriter = streamWriter
		stderrWriter = streamWriter // Combine stdout and stderr for streaming
	} else {
		stdoutWriter = &stdout
		stderrWriter = &stderr
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	// Execute
	err := cmd.Run()

	// Stop streaming writer if used
	if streamWriter != nil {
		streamWriter.Stop()
	}

	// Build result
	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("Command: %s\n", command))
	result.WriteString(fmt.Sprintf("Working Directory: %s\n\n", workingDir))

	if stdout.Len() > 0 {
		result.WriteString("STDOUT:\n")
		result.Write(stdout.Bytes())
		result.WriteString("\n")
	}

	if stderr.Len() > 0 {
		result.WriteString("STDERR:\n")
		result.Write(stderr.Bytes())
		result.WriteString("\n")
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		result.WriteString(fmt.Sprintf("Error: Command timed out after %v\n", timeout))
		return result.String(), fmt.Errorf("command timed out after %v", timeout)
	}

	if err != nil {
		result.WriteString(fmt.Sprintf("Exit Code: %v\n", err))
	} else {
		result.WriteString("Exit Code: 0\n")
	}

	return result.String(), nil
}

func SearchFiles(params map[string]interface{}, workingDir string) (string, error) {
	pattern, ok := params["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern parameter is required")
	}

	directory := workingDir
	if dir, ok := params["directory"].(string); ok && dir != "" {
		if !filepath.IsAbs(dir) {
			directory = filepath.Join(workingDir, dir)
		} else {
			directory = dir
		}
	}

	// Build grep command with options
	args := []string{"-r", "-n", "-H"}

	// Add context lines if specified
	if contextLines, ok := params["context_lines"]; ok {
		var ctx int
		switch v := contextLines.(type) {
		case int:
			ctx = v
		case float64:
			ctx = int(v)
		}
		if ctx > 0 {
			args = append(args, fmt.Sprintf("-C%d", ctx))
		}
	}

	// Add regex flag if specified
	if useRegex, ok := params["regex"].(bool); ok && useRegex {
		args = append(args, "-E")
	}

	// Add file type filters if specified
	if fileTypes, ok := params["file_types"].([]interface{}); ok && len(fileTypes) > 0 {
		for _, ft := range fileTypes {
			if ftStr, ok := ft.(string); ok {
				args = append(args, "--include=*"+ftStr)
			}
		}
	}

	// Add exclude directories if specified
	if excludeDirs, ok := params["exclude_dirs"].([]interface{}); ok && len(excludeDirs) > 0 {
		for _, ed := range excludeDirs {
			if edStr, ok := ed.(string); ok {
				args = append(args, "--exclude-dir="+edStr)
			}
		}
	}

	// Add pattern and directory
	args = append(args, pattern, directory)

	cmd := exec.Command("grep", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// grep returns exit code 1 if no matches found, which is not an error
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		return "", fmt.Errorf("grep failed: %w\n%s", err, stderr.String())
	}

	return stdout.String(), nil
}
