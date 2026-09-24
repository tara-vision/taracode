package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const scanTimeout = 300 * time.Second

var severityRank = map[string]int{"UNKNOWN": 0, "LOW": 1, "MEDIUM": 2, "HIGH": 3, "CRITICAL": 4}

// ScanTool runs a security scanner read-only. defaultSeverity applies when the call names none.
func ScanTool(defaultSeverity string) *Tool {
	return &Tool{
		Name: "scan", ReadForm: true,
		Description: "Security scan: trivy (image or filesystem), gitleaks (secrets), tfsec (terraform), kubesec " +
			"(a manifest file) or dependency (go, npm, pip, cargo, composer audit for the target directory).",
		Params: []Param{
			{Name: "scanner", Type: "string", Description: "Which scanner",
				Enum: []string{"trivy", "gitleaks", "tfsec", "kubesec", "dependency"}, Required: true},
			{Name: "target", Type: "string", Description: "Image reference, directory or file (default: working directory)"},
			{Name: "severity", Type: "string", Description: "Severity filter such as HIGH,CRITICAL"},
		},
		Classify: readOnly("scan"),
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			scanner, err := required(args, "scanner")
			if err != nil {
				return "", err
			}
			target := ScanTarget(argString(args, "target"), workingDir)
			severity := ScanSeverity(argString(args, "severity"))
			if severity == "" {
				severity = ScanSeverity(defaultSeverity)
			}
			ctx, cancel := withTimeout(ctx, scanTimeout)
			defer cancel()
			switch scanner {
			case "trivy":
				return scanOutput(trivy(ctx, workingDir, target, severity))
			case "gitleaks":
				return scanOutput(runCommand(ctx, workingDir, "gitleaks", "detect", "--source", resolvePath(target, workingDir),
					"--no-banner", "--redact", "-v", "--exit-code", "0"))
			case "tfsec":
				argv := []string{resolvePath(target, workingDir), "--no-color", "--soft-fail"}
				if severity != "" {
					argv = append(argv, "--minimum-severity", lowestSeverity(severity))
				}
				return scanOutput(runCommand(ctx, workingDir, "tfsec", argv...))
			case "kubesec":
				if target == "" {
					return "", fmt.Errorf("kubesec needs a manifest file as target")
				}
				return scanOutput(runCommand(ctx, workingDir, "kubesec", "scan", resolvePath(target, workingDir)))
			case "dependency":
				return scanOutput(dependencyAudit(ctx, resolvePath(target, workingDir)))
			}
			return "", fmt.Errorf("unknown scanner %q (trivy, gitleaks, tfsec, kubesec, dependency)", scanner)
		},
	}
}

// ScanTarget is the target of a scan call as the scan tool reads it against the working directory
// (ruling P3-R66): "" for the working directory itself, which is the default target (no target, ".",
// or its own absolute path), the path relative to it for an absolute path inside it, and any other
// target (an image, a relative path, a path outside the working directory) as given. The eval
// signature keys the same form, so a call that names the working directory by its path meets the
// fixture of the call that names none.
func ScanTarget(target, workingDir string) string {
	if !filepath.IsAbs(target) {
		if filepath.Clean(target) == "." {
			return ""
		}
		return target
	}
	if workingDir == "" {
		return target
	}
	rel, err := filepath.Rel(filepath.Clean(workingDir), filepath.Clean(target))
	switch {
	case err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return target
	case rel == ".":
		return ""
	}
	return rel
}

// ScanSeverity is a severity filter in one form: upper case, each level once, in rising order (LOW
// before HIGH before CRITICAL), with levels it does not know after the known ones in the order given.
// Scanners read the levels as a set, so CRITICAL,HIGH and HIGH,CRITICAL scan alike and key alike.
func ScanSeverity(list string) string {
	var levels []string
	seen := map[string]bool{}
	for _, level := range strings.Split(list, ",") {
		level = strings.ToUpper(strings.TrimSpace(level))
		if level != "" && !seen[level] {
			seen[level] = true
			levels = append(levels, level)
		}
	}
	sort.SliceStable(levels, func(i, j int) bool { return severityOrder(levels[i]) < severityOrder(levels[j]) })
	return strings.Join(levels, ",")
}

// severityOrder ranks a level for ScanSeverity; a level severityRank does not know ranks after all.
func severityOrder(level string) int {
	if r, ok := severityRank[level]; ok {
		return r
	}
	return len(severityRank)
}

func trivy(ctx context.Context, workingDir, target, severity string) (string, error) {
	if target == "" {
		target = "."
	}
	abs := resolvePath(target, workingDir)
	var argv []string
	if _, err := os.Stat(abs); err == nil {
		argv = []string{"fs", "--no-progress", "--scanners", "vuln,misconfig,secret", "--format", "table"}
		target = abs
	} else {
		argv = []string{"image", "--no-progress", "--format", "table"}
	}
	if severity != "" {
		argv = append(argv, "--severity", severity)
	}
	return runCommand(ctx, workingDir, "trivy", append(argv, target)...)
}

func dependencyAudit(ctx context.Context, dir string) (string, error) {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return runCommand(ctx, dir, "govulncheck", "./...")
	case exists("package-lock.json") || exists("package.json"):
		return runCommand(ctx, dir, "npm", "audit")
	case exists("Cargo.toml"):
		return runCommand(ctx, dir, "cargo", "audit")
	case exists("requirements.txt"):
		return runCommand(ctx, dir, "pip-audit", "-r", "requirements.txt")
	case exists("pyproject.toml"):
		return runCommand(ctx, dir, "pip-audit")
	case exists("composer.json"):
		return runCommand(ctx, dir, "composer", "audit")
	}
	return "", fmt.Errorf("no supported dependency manifest in %s (go.mod, package.json, Cargo.toml, "+
		"requirements.txt, pyproject.toml, composer.json)", dir)
}

// scanOutput keeps scanner findings as a result: scanners exit non-zero when they find something.
func scanOutput(out string, err error) (string, error) {
	if err != nil && strings.TrimSpace(out) != "" && strings.Contains(err.Error(), "exited with status") {
		return out + "\n[the scanner exited non-zero: findings above]", nil
	}
	return out, err
}

// lowestSeverity picks the least severe level of a comma list, for scanners that take a minimum.
func lowestSeverity(list string) string {
	best, rank := "", 99
	for _, level := range strings.Split(list, ",") {
		level = strings.TrimSpace(level)
		if r, ok := severityRank[level]; ok && r < rank {
			best, rank = level, r
		}
	}
	if best == "" {
		return list
	}
	return best
}
