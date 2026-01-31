package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ProjectTypeInfo contains detected project information
type ProjectTypeInfo struct {
	Type          string   // Primary project type (Go, Node.js, Python, etc.)
	ModuleName    string   // Project/module name
	Dependencies  []string // Main dependencies
	Frameworks    []string // Detected frameworks (docker, kubernetes, terraform, etc.)
	DetectedTools []string // Relevant taracode tools for this project
}

// ToolMapping maps frameworks/tools to relevant taracode tools
var ToolMapping = map[string][]string{
	// Infrastructure as Code
	"terraform": {"terraform_init", "terraform_plan", "terraform_apply", "terraform_destroy", "terraform_output", "terraform_state", "tfsec_scan"},
	"kubernetes": {"kubectl_get", "kubectl_apply", "kubectl_delete", "kubectl_describe", "kubectl_logs", "kubectl_exec", "kubesec_scan"},
	"helm":       {"helm_list", "helm_install", "kubectl_get", "kubectl_apply"},
	"docker":     {"docker_build", "docker_ps", "docker_logs", "docker_compose", "docker_exec", "trivy_scan"},

	// Cloud providers
	"aws":   {"aws_cli", "aws_ecs", "aws_eks"},
	"azure": {"az_cli", "az_aks"},
	"gcp":   {"gcloud", "gke"},

	// Languages (provide common development tools)
	"go":         {"execute_command", "search_files", "git_status", "git_diff", "git_commit"},
	"nodejs":     {"execute_command", "search_files", "git_status", "dependency_audit"},
	"python":     {"execute_command", "search_files", "git_status", "dependency_audit"},
	"rust":       {"execute_command", "search_files", "git_status"},
	"java":       {"execute_command", "search_files", "git_status", "dependency_audit"},
	"typescript": {"execute_command", "search_files", "git_status", "dependency_audit"},

	// Security
	"security": {"trivy_scan", "gitleaks_scan", "secrets_scan", "dependency_audit", "sast_scan"},
}

// DetectProject performs comprehensive project type and tools detection
func DetectProject(workingDir string) *ProjectTypeInfo {
	info := &ProjectTypeInfo{
		Type:          "Unknown",
		Frameworks:    []string{},
		DetectedTools: []string{},
	}

	// Detect primary project type
	detectPrimaryType(workingDir, info)

	// Detect frameworks and infrastructure tools
	detectFrameworks(workingDir, info)

	// Map detected items to taracode tools
	mapToTools(info)

	// Always include core file operations
	coreTools := []string{"read_file", "write_file", "edit_file", "list_files", "find_files"}
	for _, tool := range coreTools {
		if !contains(info.DetectedTools, tool) {
			info.DetectedTools = append(info.DetectedTools, tool)
		}
	}

	return info
}

func detectPrimaryType(workingDir string, info *ProjectTypeInfo) {
	// Go project
	if content, err := os.ReadFile(filepath.Join(workingDir, "go.mod")); err == nil {
		info.Type = "Go"
		lines := strings.Split(string(content), "\n")
		if len(lines) > 0 {
			info.ModuleName = strings.TrimPrefix(strings.TrimSpace(lines[0]), "module ")
		}
		// Extract dependencies
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "require ") || (strings.HasPrefix(line, "\t") && strings.Contains(line, " v")) {
				parts := strings.Fields(line)
				if len(parts) >= 1 {
					dep := strings.TrimPrefix(parts[0], "require")
					dep = strings.TrimSpace(dep)
					if dep != "" && dep != "(" && dep != ")" {
						info.Dependencies = append(info.Dependencies, dep)
					}
				}
			}
		}
		return
	}

	// Node.js project
	if content, err := os.ReadFile(filepath.Join(workingDir, "package.json")); err == nil {
		info.Type = "Node.js"
		var pkg map[string]interface{}
		if json.Unmarshal(content, &pkg) == nil {
			if name, ok := pkg["name"].(string); ok {
				info.ModuleName = name
			}
			// Check for TypeScript
			if deps, ok := pkg["devDependencies"].(map[string]interface{}); ok {
				if _, hasTS := deps["typescript"]; hasTS {
					info.Type = "TypeScript"
				}
			}
			// Extract dependencies
			if deps, ok := pkg["dependencies"].(map[string]interface{}); ok {
				for dep := range deps {
					info.Dependencies = append(info.Dependencies, dep)
				}
			}
		}
		return
	}

	// Python project
	if _, err := os.Stat(filepath.Join(workingDir, "pyproject.toml")); err == nil {
		info.Type = "Python"
		return
	}
	if _, err := os.Stat(filepath.Join(workingDir, "requirements.txt")); err == nil {
		info.Type = "Python"
		return
	}
	if _, err := os.Stat(filepath.Join(workingDir, "setup.py")); err == nil {
		info.Type = "Python"
		return
	}

	// Rust project
	if content, err := os.ReadFile(filepath.Join(workingDir, "Cargo.toml")); err == nil {
		info.Type = "Rust"
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "name = ") {
				info.ModuleName = strings.Trim(strings.TrimPrefix(line, "name = "), `"'`)
				break
			}
		}
		return
	}

	// Java project (Maven or Gradle)
	if _, err := os.Stat(filepath.Join(workingDir, "pom.xml")); err == nil {
		info.Type = "Java"
		return
	}
	if _, err := os.Stat(filepath.Join(workingDir, "build.gradle")); err == nil {
		info.Type = "Java"
		return
	}
	if _, err := os.Stat(filepath.Join(workingDir, "build.gradle.kts")); err == nil {
		info.Type = "Kotlin"
		return
	}

	// Terraform project (if no other primary type found)
	if hasTerraformFiles(workingDir) {
		info.Type = "Terraform"
		return
	}
}

func detectFrameworks(workingDir string, info *ProjectTypeInfo) {
	// Docker
	if _, err := os.Stat(filepath.Join(workingDir, "Dockerfile")); err == nil {
		info.Frameworks = append(info.Frameworks, "docker")
	}
	if _, err := os.Stat(filepath.Join(workingDir, "docker-compose.yml")); err == nil {
		info.Frameworks = append(info.Frameworks, "docker")
	}
	if _, err := os.Stat(filepath.Join(workingDir, "docker-compose.yaml")); err == nil {
		if !contains(info.Frameworks, "docker") {
			info.Frameworks = append(info.Frameworks, "docker")
		}
	}

	// Kubernetes
	if hasKubernetesFiles(workingDir) {
		info.Frameworks = append(info.Frameworks, "kubernetes")
	}

	// Helm
	if _, err := os.Stat(filepath.Join(workingDir, "Chart.yaml")); err == nil {
		info.Frameworks = append(info.Frameworks, "helm")
	}
	if _, err := os.Stat(filepath.Join(workingDir, "charts")); err == nil {
		info.Frameworks = append(info.Frameworks, "helm")
	}

	// Terraform
	if hasTerraformFiles(workingDir) && info.Type != "Terraform" {
		info.Frameworks = append(info.Frameworks, "terraform")
	}

	// AWS
	if hasAWSFiles(workingDir) {
		info.Frameworks = append(info.Frameworks, "aws")
	}

	// Azure
	if hasAzureFiles(workingDir) {
		info.Frameworks = append(info.Frameworks, "azure")
	}

	// GCP
	if hasGCPFiles(workingDir) {
		info.Frameworks = append(info.Frameworks, "gcp")
	}
}

func hasTerraformFiles(workingDir string) bool {
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tf") {
			return true
		}
	}
	// Check subdirectories one level deep
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			subEntries, err := os.ReadDir(filepath.Join(workingDir, entry.Name()))
			if err != nil {
				continue
			}
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() && strings.HasSuffix(subEntry.Name(), ".tf") {
					return true
				}
			}
		}
	}
	return false
}

func hasKubernetesFiles(workingDir string) bool {
	// Check for common k8s directories
	k8sDirs := []string{"k8s", "kubernetes", "deploy", "manifests", "kustomize"}
	for _, dir := range k8sDirs {
		if _, err := os.Stat(filepath.Join(workingDir, dir)); err == nil {
			return true
		}
	}

	// Check for files with k8s patterns
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, "deployment") || strings.Contains(name, "service") ||
			strings.Contains(name, "ingress") || strings.Contains(name, "configmap") ||
			name == "kustomization.yaml" || name == "kustomization.yml" {
			return true
		}
	}
	return false
}

func hasAWSFiles(workingDir string) bool {
	awsIndicators := []string{
		".aws",
		"aws.json",
		"aws-config.json",
		"samconfig.toml",
		"template.yaml", // SAM template
		"cloudformation",
		"cdk.json",
	}
	for _, indicator := range awsIndicators {
		if _, err := os.Stat(filepath.Join(workingDir, indicator)); err == nil {
			return true
		}
	}
	// Check terraform files for AWS provider
	if hasTerraformAWSProvider(workingDir) {
		return true
	}
	return false
}

func hasAzureFiles(workingDir string) bool {
	azureIndicators := []string{
		".azure",
		"azure-pipelines.yml",
		"azure-pipelines.yaml",
		"azuredeploy.json",
	}
	for _, indicator := range azureIndicators {
		if _, err := os.Stat(filepath.Join(workingDir, indicator)); err == nil {
			return true
		}
	}
	return false
}

func hasGCPFiles(workingDir string) bool {
	gcpIndicators := []string{
		".gcloud",
		"app.yaml", // App Engine
		"cloudbuild.yaml",
		"cloudbuild.yml",
	}
	for _, indicator := range gcpIndicators {
		if _, err := os.Stat(filepath.Join(workingDir, indicator)); err == nil {
			return true
		}
	}
	return false
}

func hasTerraformAWSProvider(workingDir string) bool {
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tf") {
			content, err := os.ReadFile(filepath.Join(workingDir, entry.Name()))
			if err != nil {
				continue
			}
			if strings.Contains(string(content), `provider "aws"`) ||
				strings.Contains(string(content), "aws_") {
				return true
			}
		}
	}
	return false
}

func mapToTools(info *ProjectTypeInfo) {
	toolSet := make(map[string]bool)

	// Map primary project type to tools
	if tools, ok := ToolMapping[strings.ToLower(info.Type)]; ok {
		for _, tool := range tools {
			toolSet[tool] = true
		}
	}

	// Map frameworks to tools
	for _, framework := range info.Frameworks {
		if tools, ok := ToolMapping[framework]; ok {
			for _, tool := range tools {
				toolSet[tool] = true
			}
		}
	}

	// Convert set to slice
	for tool := range toolSet {
		info.DetectedTools = append(info.DetectedTools, tool)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
