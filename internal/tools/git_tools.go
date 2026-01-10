package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// GitStatus shows the status of the git repository
func GitStatus(params map[string]interface{}, workingDir string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git status failed: %w\n%s", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		return "Working tree clean - no changes to commit", nil
	}

	var result strings.Builder
	result.WriteString("Git Status:\n")
	result.WriteString(output)

	return result.String(), nil
}

// GitDiff shows the diff for the repository or a specific file
func GitDiff(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"diff"}

	// Add file path if specified
	if filePath, ok := params["file_path"].(string); ok && filePath != "" {
		args = append(args, filePath)
	}

	// Add --staged flag if specified
	if staged, ok := params["staged"].(bool); ok && staged {
		args = append([]string{"diff", "--staged"}, args[1:]...)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w\n%s", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		return "No changes to show", nil
	}

	return output, nil
}

// GitLog shows recent commit history
func GitLog(params map[string]interface{}, workingDir string) (string, error) {
	limit := 10
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	} else if l, ok := params["limit"].(int); ok {
		limit = l
	}

	cmd := exec.Command("git", "log", fmt.Sprintf("-n%d", limit), "--pretty=format:%h - %s (%an, %ar)")
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git log failed: %w\n%s", err, stderr.String())
	}

	return stdout.String(), nil
}

// GitAdd stages files for commit
func GitAdd(params map[string]interface{}, workingDir string) (string, error) {
	files, ok := params["files"].([]interface{})
	if !ok || len(files) == 0 {
		return "", fmt.Errorf("files parameter is required and must be an array")
	}

	fileStrings := make([]string, 0, len(files))
	for _, f := range files {
		if fileStr, ok := f.(string); ok {
			fileStrings = append(fileStrings, fileStr)
		}
	}

	if len(fileStrings) == 0 {
		return "", fmt.Errorf("no valid file paths provided")
	}

	args := append([]string{"add"}, fileStrings...)
	cmd := exec.Command("git", args...)
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git add failed: %w\n%s", err, stderr.String())
	}

	return fmt.Sprintf("Staged %d files for commit", len(fileStrings)), nil
}

// GitCommit creates a commit with staged changes
func GitCommit(params map[string]interface{}, workingDir string) (string, error) {
	message, ok := params["message"].(string)
	if !ok || message == "" {
		return "", fmt.Errorf("message parameter is required")
	}

	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git commit failed: %w\n%s\n%s", err, stdout.String(), stderr.String())
	}

	return fmt.Sprintf("Commit created:\n%s", stdout.String()), nil
}

// GitBranch shows current branch and lists all branches
func GitBranch(params map[string]interface{}, workingDir string) (string, error) {
	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git branch failed: %w\n%s", err, stderr.String())
	}

	return stdout.String(), nil
}
