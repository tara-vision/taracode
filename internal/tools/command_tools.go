package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

const defaultCommandTimeout = 60 * time.Second

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

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create command with context
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workingDir

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute
	err := cmd.Run()

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
