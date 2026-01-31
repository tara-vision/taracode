package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// =============================================================================
// SECURITY SCANNING TOOLS
// =============================================================================

// TrivyScan runs Trivy vulnerability scanner on images, filesystems, or configs
func TrivyScan(params map[string]interface{}, workingDir string) (string, error) {
	target, _ := params["target"].(string)
	if target == "" {
		return "", fmt.Errorf("target parameter is required (e.g., image:tag, . for filesystem)")
	}

	// Scan type: image (default), fs, config, repo
	scanType, _ := params["type"].(string)
	if scanType == "" {
		scanType = "image"
	}

	args := []string{scanType, target}

	// Severity filter (UNKNOWN, LOW, MEDIUM, HIGH, CRITICAL)
	if severity, ok := params["severity"].(string); ok && severity != "" {
		args = append(args, "--severity", severity)
	}

	// Output format (default: table)
	format := "table"
	if f, ok := params["format"].(string); ok && f != "" {
		format = f
	}
	args = append(args, "--format", format)

	// Ignore unfixed vulnerabilities
	if ignoreUnfixed, ok := params["ignore_unfixed"].(bool); ok && ignoreUnfixed {
		args = append(args, "--ignore-unfixed")
	}

	return runCommand("trivy", args, workingDir, 120*time.Second)
}

// GitleaksScan runs gitleaks to detect secrets in git repositories
func GitleaksScan(params map[string]interface{}, workingDir string) (string, error) {
	// Source path (default: current directory)
	path, _ := params["path"].(string)
	if path == "" {
		path = "."
	}

	args := []string{"detect", "--source", path}

	// Verbose output
	if verbose, ok := params["verbose"].(bool); ok && verbose {
		args = append(args, "-v")
	}

	// Report format (json, csv, sarif)
	if format, ok := params["format"].(string); ok && format != "" {
		args = append(args, "--report-format", format)
	}

	// Baseline file for ignoring known secrets
	if baseline, ok := params["baseline"].(string); ok && baseline != "" {
		args = append(args, "--baseline-path", baseline)
	}

	// No git (scan files without git history)
	if noGit, ok := params["no_git"].(bool); ok && noGit {
		args = append(args, "--no-git")
	}

	return runCommand("gitleaks", args, workingDir, 60*time.Second)
}

// SecretsScan searches for hardcoded secrets using common patterns
func SecretsScan(params map[string]interface{}, workingDir string) (string, error) {
	// Path to scan
	path, _ := params["path"].(string)
	if path == "" {
		path = "."
	}

	// Common secret patterns to search for
	defaultPatterns := []string{
		"password",
		"api_key",
		"apikey",
		"secret",
		"token",
		"private_key",
		"aws_access_key",
		"aws_secret",
		"client_secret",
	}

	// Allow custom patterns
	patterns := defaultPatterns
	if customPatterns, ok := params["patterns"].([]interface{}); ok && len(customPatterns) > 0 {
		patterns = []string{}
		for _, p := range customPatterns {
			if s, ok := p.(string); ok {
				patterns = append(patterns, s)
			}
		}
	}

	// Build grep pattern
	pattern := strings.Join(patterns, "|")

	// Exclusions for common false positives
	excludeArgs := []string{
		"--exclude-dir=.git",
		"--exclude-dir=node_modules",
		"--exclude-dir=vendor",
		"--exclude-dir=.taracode",
		"--exclude=*.min.js",
		"--exclude=*.lock",
	}

	args := append([]string{"-riE", pattern, path}, excludeArgs...)

	// Use grep for pattern matching
	result, err := runCommand("grep", args, workingDir, 30*time.Second)

	// grep returns exit code 1 if no matches (not an error)
	if err != nil && result == "" {
		return "No potential secrets found.", nil
	}

	if result == "" {
		return "No potential secrets found.", nil
	}

	return "Potential secrets found:\n" + result, nil
}

// DependencyAudit runs dependency vulnerability checks for various package managers
func DependencyAudit(params map[string]interface{}, workingDir string) (string, error) {
	auditType, _ := params["type"].(string)
	if auditType == "" {
		return "", fmt.Errorf("type parameter is required (npm, pip, go, cargo, composer)")
	}

	// Optional path override
	path, _ := params["path"].(string)
	if path != "" {
		workingDir = filepath.Join(workingDir, path)
	}

	switch auditType {
	case "npm":
		// npm audit with JSON output for structured results
		args := []string{"audit"}
		if json, ok := params["json"].(bool); ok && json {
			args = append(args, "--json")
		}
		return runCommand("npm", args, workingDir, 60*time.Second)

	case "pip":
		// pip-audit for Python dependencies
		args := []string{}
		if format, ok := params["format"].(string); ok && format != "" {
			args = append(args, "--format", format)
		}
		return runCommand("pip-audit", args, workingDir, 60*time.Second)

	case "go":
		// govulncheck for Go modules
		args := []string{"./..."}
		return runCommand("govulncheck", args, workingDir, 120*time.Second)

	case "cargo":
		// cargo audit for Rust dependencies
		return runCommand("cargo", []string{"audit"}, workingDir, 60*time.Second)

	case "composer":
		// composer audit for PHP dependencies
		return runCommand("composer", []string{"audit"}, workingDir, 60*time.Second)

	default:
		return "", fmt.Errorf("unsupported audit type: %s (supported: npm, pip, go, cargo, composer)", auditType)
	}
}

// SASTScan runs static application security testing using semgrep
func SASTScan(params map[string]interface{}, workingDir string) (string, error) {
	// Path to scan (default: current directory)
	path, _ := params["path"].(string)
	if path == "" {
		path = "."
	}

	args := []string{"scan"}

	// Config/ruleset (default: auto for language detection)
	config := "auto"
	if c, ok := params["config"].(string); ok && c != "" {
		config = c
	}
	args = append(args, "--config", config)

	// Add path at the end
	args = append(args, path)

	// Output format
	if format, ok := params["format"].(string); ok && format != "" {
		args = append(args, "--output-format", format)
	}

	// Severity filter
	if severity, ok := params["severity"].(string); ok && severity != "" {
		args = append(args, "--severity", severity)
	}

	return runCommand("semgrep", args, workingDir, 120*time.Second)
}

// TfsecScan runs Terraform security scanner
func TfsecScan(params map[string]interface{}, workingDir string) (string, error) {
	// Path to Terraform files (default: current directory)
	path, _ := params["path"].(string)
	if path == "" {
		path = "."
	}

	args := []string{path}

	// Output format (default, json, csv, checkstyle, junit, sarif)
	if format, ok := params["format"].(string); ok && format != "" {
		args = append(args, "--format", format)
	}

	// Minimum severity (CRITICAL, HIGH, MEDIUM, LOW)
	if minSeverity, ok := params["minimum_severity"].(string); ok && minSeverity != "" {
		args = append(args, "--minimum-severity", minSeverity)
	}

	// Exclude specific checks
	if exclude, ok := params["exclude"].(string); ok && exclude != "" {
		args = append(args, "--exclude", exclude)
	}

	return runCommand("tfsec", args, workingDir, 60*time.Second)
}

// KubesecScan runs Kubernetes manifest security scanner
func KubesecScan(params map[string]interface{}, workingDir string) (string, error) {
	file, _ := params["file"].(string)
	if file == "" {
		return "", fmt.Errorf("file parameter is required (path to Kubernetes manifest)")
	}

	args := []string{"scan", file}

	// Output format (json, template)
	if format, ok := params["format"].(string); ok && format != "" {
		args = append(args, "-o", format)
	}

	return runCommand("kubesec", args, workingDir, 30*time.Second)
}
