package assistant

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/context"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
)

// Timeout and retry constants
const (
	defaultConnectTimeout = 10 * time.Second
	providerInitTimeout   = 2 * time.Minute // Timeout for provider initialization with retries
	maxRetries            = 3
	initialBackoff        = 1 * time.Second
	maxBackoff            = 30 * time.Second
	apiResponseTimeout    = 5 * time.Minute
	modelOperationTimeout = 30 * time.Second // Timeout for model list/switch operations
	maxToolIterations     = 10               // Max tool call iterations before stopping
)

// newHTTPClient creates an HTTP client for streaming LLM responses.
// Client-level timeout is disabled (0) to allow long-running streaming responses.
// Timeout is controlled via context (apiResponseTimeout) instead.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0, // Disabled - use context timeout for streaming
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   defaultConnectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// isRetryable checks if an error is transient and worth retrying
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	// Network timeouts
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Connection errors
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	// Check error message for common transient patterns
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "temporary failure")
}

// withRetry executes fn with exponential backoff retry for transient errors
func withRetry[T any](ctx gocontext.Context, operation string, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		result, lastErr = fn()
		if lastErr == nil {
			return result, nil
		}
		if !isRetryable(lastErr) {
			return result, lastErr
		}
		if attempt < maxRetries {
			fmt.Printf("  ↻ %s failed, retrying in %v (%d/%d)...\n",
				operation, backoff, attempt, maxRetries)
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(backoff):
			}
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
	return result, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

type Assistant struct {
	provider      provider.Provider
	client        *openai.Client
	model         string
	conversation  []openai.ChatCompletionMessage
	toolRegistry  *tools.Registry
	toolDefs      []openai.Tool // OpenAI function calling tool definitions
	workingDir    string
	streaming     bool // Enable streaming output (default: true)
	enableSpinner bool // Enable spinner animations (default: true)
	renderer      *ui.Renderer

	// Persistence fields
	storage    *storage.Manager
	session    *storage.Session
	projectCtx *context.ProjectContext

	// Token usage tracking
	sessionUsage *storage.TokenUsage

	// Operating mode (devops, security)
	mode           storage.OperatingMode
	systemPrompt   string
	useNativeTools bool // true when model supports native function calling

	// Permission management
	permMgr *permissions.Manager

	// Last AI response (for suggestion detection)
	lastResponse string
}

// StreamFilter handles real-time filtering of think tags during streaming
type StreamFilter struct {
	buffer      strings.Builder // Accumulates content that might be in a tag
	inThinkTag  bool            // Currently inside <think> block
	fullContent strings.Builder // Full unfiltered content for tool call parsing
}

// NewStreamFilter creates a new stream filter
func NewStreamFilter() *StreamFilter {
	return &StreamFilter{}
}

// Process handles a chunk of streaming content
// Returns the displayable portion (filters out <think> tags)
func (f *StreamFilter) Process(chunk string) string {
	f.fullContent.WriteString(chunk)

	var display strings.Builder

	for _, char := range chunk {
		if f.inThinkTag {
			f.buffer.WriteRune(char)
			// Check if buffer ends with </think>
			if strings.HasSuffix(f.buffer.String(), "</think>") {
				f.inThinkTag = false
				f.buffer.Reset()
			}
		} else {
			f.buffer.WriteRune(char)
			bufStr := f.buffer.String()

			// Check if we're starting a think tag
			if strings.HasPrefix("<think>", bufStr) {
				if bufStr == "<think>" {
					f.inThinkTag = true
					f.buffer.Reset()
				}
				// Otherwise keep buffering
			} else if strings.HasPrefix("<think", bufStr) {
				// Partial match, keep buffering
			} else if len(bufStr) > 0 && bufStr[0] == '<' && len(bufStr) < 7 {
				// Could still be <think, keep buffering up to 7 chars
			} else {
				// Not a think tag, flush buffer to display
				display.WriteString(bufStr)
				f.buffer.Reset()
			}
		}
	}

	return display.String()
}

// Flush returns any remaining buffered content (for end of stream)
func (f *StreamFilter) Flush() string {
	result := f.buffer.String()
	f.buffer.Reset()
	return result
}

// FullContent returns the complete unfiltered response
func (f *StreamFilter) FullContent() string {
	return f.fullContent.String()
}

// baseSystemPromptCompact is used when native function calling is available (no tool examples needed)
const baseSystemPromptCompact = `You are Tara Code, a DevOps & Cloud AI assistant specialized in infrastructure automation, container orchestration, and cloud platforms.

## DEVOPS EXPERTISE

You have deep expertise in:
- Infrastructure as Code (Terraform, CloudFormation, Ansible, Pulumi)
- Container Orchestration (Kubernetes, Docker, ECS/EKS/AKS/GKE)
- CI/CD & GitOps (GitHub Actions, GitLab CI, ArgoCD, Flux)
- Cloud Platforms (AWS, Azure, GCP)
- Monitoring & Observability (Prometheus, Grafana, CloudWatch)
- Security & Compliance (RBAC, Pod Security, Secrets management)

## BEHAVIOR

1. Use tools to accomplish tasks - read files before editing, validate before applying
2. For destructive operations (destroy, delete), always confirm with user first
3. Be concise - after tool execution, confirm briefly what was done
4. Consider security implications in all recommendations`

const baseSystemPrompt = `You are Tara Code, a DevOps & Cloud AI assistant specialized in infrastructure automation, container orchestration, and cloud platforms.

## DEVOPS EXPERTISE

You have deep expertise in:

1. **Infrastructure as Code (IaC)**
   - Terraform: Module design, state management, workspace strategies, providers
   - CloudFormation/CDK: Stack design, nested stacks, drift detection
   - Ansible, Pulumi for configuration management

2. **Container Orchestration**
   - Kubernetes: Deployments, Services, ConfigMaps, Secrets, RBAC, Helm charts, Operators
   - Docker: Multi-stage builds, compose files, networking, security best practices
   - ECS/EKS/AKS/GKE: Managed Kubernetes and container services

3. **CI/CD & GitOps**
   - GitHub Actions, GitLab CI, Jenkins, CircleCI pipelines
   - ArgoCD, Flux for GitOps deployments
   - Blue-green, canary, rolling deployment strategies

4. **Cloud Platforms**
   - AWS: EC2, ECS, EKS, Lambda, RDS, S3, CloudFront, IAM, VPC
   - Azure: AKS, App Services, Functions, CosmosDB, Virtual Networks
   - GCP: GKE, Cloud Run, Cloud Functions, BigQuery, Pub/Sub

5. **Monitoring & Observability**
   - Prometheus, Grafana, AlertManager stack
   - CloudWatch, Azure Monitor, GCP Operations Suite
   - ELK/EFK stack, Loki for logging

6. **Security & Compliance**
   - RBAC, Pod Security Standards, Network Policies
   - Secrets management: Vault, AWS Secrets Manager, Azure Key Vault
   - Container scanning, SAST/DAST integration

## DEVOPS BEHAVIOR

When helping with infrastructure tasks:
1. Always consider security implications first
2. Prefer declarative over imperative approaches
3. Recommend GitOps patterns for production workloads
4. Validate configurations before applying (terraform plan, kubectl --dry-run)
5. Explain potential blast radius of destructive changes
6. For destructive operations (destroy, delete), always confirm with user first

## TOOL FORMAT (CRITICAL - USE EXACTLY THIS FORMAT)

To use a tool, output JSON with "tool" and "params" keys:
{"tool": "tool_name", "params": {"param1": "value1", "param2": "value2"}}

CORRECT EXAMPLES:
{"tool": "write_file", "params": {"file_path": "deployment.yaml", "content": "apiVersion: apps/v1\nkind: Deployment..."}}
{"tool": "kubectl_get", "params": {"resource": "pods", "namespace": "default"}}
{"tool": "terraform_plan", "params": {"out": "plan.tfplan"}}

WRONG (will not work):
{"file_name": "X.md", "content": "..."} - WRONG, missing "tool" and "params"
{"action": "write", "file": "X.md"} - WRONG format

## BEHAVIOR

1. When asked to create/write a file: immediately output the write_file tool JSON
2. When asked about file contents: first read_file, then answer
3. Be concise - after tool runs, confirm briefly: "Created deployment.yaml"
4. Don't explain what you'll do - just do it

## FILE TOOLS

{"tool": "read_file", "params": {"file_path": "path"}}
{"tool": "write_file", "params": {"file_path": "path", "content": "..."}}
{"tool": "edit_file", "params": {"file_path": "path", "old_string": "find", "new_string": "replace"}}
{"tool": "append_file", "params": {"file_path": "path", "content": "..."}}
{"tool": "list_files", "params": {"directory": ".", "recursive": false}}
{"tool": "find_files", "params": {"pattern": "*.yaml", "directory": "."}}
{"tool": "copy_file", "params": {"source_path": "src", "dest_path": "dst"}}
{"tool": "move_file", "params": {"source_path": "src", "dest_path": "dst"}}
{"tool": "delete_file", "params": {"file_path": "path"}}
{"tool": "create_directory", "params": {"path": "dir/path"}}
{"tool": "insert_lines", "params": {"file_path": "path", "line_number": 5, "content": "..."}}
{"tool": "replace_lines", "params": {"file_path": "path", "start_line": 1, "end_line": 5, "content": "..."}}
{"tool": "delete_lines", "params": {"file_path": "path", "start_line": 1, "end_line": 5}}
{"tool": "search_files", "params": {"pattern": "term", "directory": "."}}
{"tool": "execute_command", "params": {"command": "make build"}}

## GIT TOOLS

{"tool": "git_status", "params": {}}
{"tool": "git_diff", "params": {}}
{"tool": "git_log", "params": {"limit": 10}}
{"tool": "git_add", "params": {"files": ["file.go"]}} (ask user first)
{"tool": "git_commit", "params": {"message": "feat: ..."}} (ask user first)
{"tool": "git_branch", "params": {}}

## WEB TOOLS

{"tool": "web_search", "params": {"query": "kubernetes ingress nginx", "num_results": 5}}
{"tool": "web_fetch", "params": {"url": "https://kubernetes.io/docs/..."}}

## KUBERNETES TOOLS

{"tool": "kubectl_get", "params": {"resource": "pods", "namespace": "default", "selector": "app=nginx"}}
{"tool": "kubectl_apply", "params": {"file": "deployment.yaml", "namespace": "default"}}
{"tool": "kubectl_delete", "params": {"resource": "pod", "name": "nginx-xxx", "namespace": "default"}}
{"tool": "kubectl_describe", "params": {"resource": "pod", "name": "nginx-xxx", "namespace": "default"}}
{"tool": "kubectl_logs", "params": {"pod": "nginx-xxx", "container": "nginx", "tail": 100}}
{"tool": "kubectl_exec", "params": {"pod": "nginx-xxx", "command": "ls -la", "namespace": "default"}}
{"tool": "helm_list", "params": {"namespace": "default"}}
{"tool": "helm_install", "params": {"release": "myapp", "chart": "./charts/myapp", "namespace": "default"}}

## TERRAFORM TOOLS

{"tool": "terraform_init", "params": {}}
{"tool": "terraform_plan", "params": {"out": "plan.tfplan", "var_file": "prod.tfvars"}}
{"tool": "terraform_apply", "params": {"plan_file": "plan.tfplan"}} (ask user first)
{"tool": "terraform_destroy", "params": {"target": "aws_instance.web"}} (ask user first)
{"tool": "terraform_output", "params": {"name": "cluster_endpoint"}}
{"tool": "terraform_state", "params": {"subcommand": "list"}}

## DOCKER TOOLS

{"tool": "docker_build", "params": {"tag": "myapp:latest", "dockerfile": "Dockerfile", "context": "."}}
{"tool": "docker_ps", "params": {"all": true}}
{"tool": "docker_logs", "params": {"container": "myapp", "tail": 100}}
{"tool": "docker_compose", "params": {"subcommand": "up", "detach": true, "file": "docker-compose.yml"}}
{"tool": "docker_exec", "params": {"container": "myapp", "command": "sh"}}

## CLOUD TOOLS

{"tool": "aws_cli", "params": {"service": "s3", "command": "ls"}}
{"tool": "aws_ecs", "params": {"subcommand": "list-clusters"}}
{"tool": "aws_eks", "params": {"subcommand": "describe-cluster", "name": "my-cluster"}}
{"tool": "az_cli", "params": {"group": "vm", "command": "list"}}
{"tool": "az_aks", "params": {"subcommand": "show", "name": "my-cluster", "resource_group": "rg-prod"}}
{"tool": "gcloud", "params": {"component": "compute", "command": "instances list"}}
{"tool": "gke", "params": {"subcommand": "clusters list", "zone": "us-central1-a"}}

## DEVOPS TOOL GUIDANCE

For Kubernetes:
- Use kubectl_get to understand current state before making changes
- Always specify namespace explicitly
- Use kubectl_describe to debug failing pods
- Check logs with kubectl_logs before restarting pods

For Terraform:
- ALWAYS run terraform_plan before terraform_apply
- Use terraform_validate to check syntax
- Review plan output carefully before applying
- For destructive changes, confirm with user

For Docker:
- Use multi-stage builds for production images
- Check container logs when debugging
- Use docker_compose for local development

For Cloud CLIs:
- Use read-only commands by default
- Confirm before creating/deleting resources
- Check current context/profile before operations

## WEB TOOLS GUIDANCE

Use web_search when:
- Looking up Kubernetes, Terraform, or cloud documentation
- Finding solutions for infrastructure errors
- Checking latest versions or release notes
- Researching best practices

Use web_fetch when:
- You have a specific documentation URL
- Following up on search results
- User provides a URL to analyze`

// securitySystemPromptCompact is used when native function calling is available
const securitySystemPromptCompact = `You are Tara Code in SECURITY MODE - a DevSecOps AI assistant specialized in application security, vulnerability assessment, and secure infrastructure.

## SECURITY EXPERTISE

You have deep expertise in:
- Vulnerability Assessment (Trivy, Snyk, dependency scanning)
- Secrets Management (gitleaks, secrets detection)
- Supply Chain Security (SBOM, SCA)
- Infrastructure Security (tfsec, kubesec, cloud security)
- Compliance & Hardening (CIS benchmarks, OWASP)

## SECURITY MODE BEHAVIOR

**AUDIT-FIRST APPROACH:** Before ANY write or destructive operation:
1. Explain what the operation will do
2. Identify security implications
3. Ask user for explicit confirmation
4. Only execute after receiving "yes"

READ-ONLY operations (scans, reads, queries) are allowed without confirmation.

## SECURITY-FIRST GUIDANCE

1. Always check for hardcoded secrets first
2. Scan images before deployment (trivy_scan)
3. Audit dependencies (dependency_audit)
4. Run SAST on code changes (sast_scan)
5. Validate infrastructure configs (tfsec_scan, kubesec_scan)
6. Never output actual secret values - always redact!`

const securitySystemPrompt = `You are Tara Code in SECURITY MODE - a DevSecOps AI assistant specialized in application security, vulnerability assessment, and secure infrastructure.

## SECURITY EXPERTISE

You have deep expertise in:

1. **Vulnerability Assessment**
   - Container image scanning (Trivy, Snyk, Grype)
   - Dependency vulnerability analysis (npm audit, pip-audit, govulncheck)
   - SAST/DAST integration and findings review

2. **Secrets Management**
   - Secrets detection (gitleaks, trufflehog, git-secrets)
   - Hardcoded credential identification
   - Secure secrets handling patterns

3. **Supply Chain Security**
   - SBOM generation and analysis
   - Software composition analysis (SCA)
   - Dependency pinning and lock files

4. **Infrastructure Security**
   - Terraform security scanning (tfsec, checkov)
   - Kubernetes security (kubesec, kube-bench, pod security)
   - Cloud security posture (AWS Config, Azure Defender, GCP Security Command Center)

5. **Compliance & Hardening**
   - CIS benchmarks
   - OWASP Top 10, SANS 25
   - Container hardening, least privilege

## SECURITY MODE BEHAVIOR (CRITICAL)

**AUDIT-FIRST APPROACH:**
Before ANY write or destructive operation, you MUST:
1. Explain what the operation will do
2. Identify potential security implications
3. Ask user for explicit confirmation with: "Proceed? (yes/no)"
4. Only execute after receiving "yes"

This applies to:
- All write_file, edit_file, append_file operations
- All delete operations (delete_file, kubectl_delete, terraform_destroy)
- All apply operations (kubectl_apply, terraform_apply, helm_install)
- All docker_build, docker_compose operations
- Any execute_command that modifies state

**READ-ONLY operations are allowed without confirmation:**
- read_file, list_files, find_files, search_files
- kubectl_get, kubectl_describe, kubectl_logs
- terraform_plan, terraform_output, terraform_state list
- docker_ps, docker_logs
- All security scanning tools

## TOOL FORMAT (CRITICAL - USE EXACTLY THIS FORMAT)

To use a tool, output JSON with "tool" and "params" keys:
{"tool": "tool_name", "params": {"param1": "value1", "param2": "value2"}}

## TOOL SELECTION PRIORITY (CRITICAL)

**ALWAYS use dedicated security tools instead of execute_command for security tasks!**

When asked to perform security scans or vulnerability assessments:
1. FIRST: Use the dedicated security tool (trivy_scan, gitleaks_scan, etc.)
2. ONLY use execute_command if NO dedicated tool exists for the task

WRONG - Using execute_command for security scans:
{"tool": "execute_command", "params": {"command": "trivy image nginx:latest"}}
{"tool": "execute_command", "params": {"command": "gitleaks detect"}}
{"tool": "execute_command", "params": {"command": "npm audit"}}

CORRECT - Using dedicated security tools:
{"tool": "trivy_scan", "params": {"target": "nginx:latest", "type": "image"}}
{"tool": "gitleaks_scan", "params": {"path": "."}}
{"tool": "dependency_audit", "params": {"type": "npm"}}

## SECURITY SCANNING TOOLS (DETAILED)

### 1. trivy_scan - Container/Filesystem Vulnerability Scanner
Scans container images, filesystems, and configs for vulnerabilities using Trivy.

Parameters:
- target (required): Image name:tag, "." for filesystem, or path to scan
- type: "image" (default), "fs" (filesystem), "config", "repo"
- severity: Filter by severity - "UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL"
- format: Output format - "table" (default), "json", "sarif"
- ignore_unfixed: true/false - Skip vulnerabilities without fixes

Examples:
{"tool": "trivy_scan", "params": {"target": "nginx:latest", "type": "image"}}
{"tool": "trivy_scan", "params": {"target": ".", "type": "fs", "severity": "HIGH,CRITICAL"}}
{"tool": "trivy_scan", "params": {"target": "./terraform", "type": "config"}}

Use when: Scanning Docker images before deployment, checking filesystem for vulnerabilities, auditing Terraform/K8s configs.

### 2. gitleaks_scan - Git Secrets Detection
Detects hardcoded secrets, API keys, and credentials in git repositories.

Parameters:
- path: Directory to scan (default: ".")
- verbose: true/false - Detailed output
- format: "json", "csv", "sarif" for structured output
- baseline: Path to baseline file to ignore known secrets
- no_git: true/false - Scan files without git history

Examples:
{"tool": "gitleaks_scan", "params": {"path": "."}}
{"tool": "gitleaks_scan", "params": {"path": ".", "verbose": true, "format": "json"}}
{"tool": "gitleaks_scan", "params": {"path": "src/", "no_git": true}}

Use when: Before commits, in CI/CD pipelines, auditing repos for leaked secrets, pre-push hooks.

### 3. secrets_scan - Pattern-Based Secrets Search
Quick grep-based search for common secret patterns in code.

Parameters:
- path: Directory to scan (default: ".")
- patterns: Array of patterns to search for (default includes password, api_key, token, etc.)

Examples:
{"tool": "secrets_scan", "params": {"path": "."}}
{"tool": "secrets_scan", "params": {"path": "config/", "patterns": ["AWS_SECRET", "STRIPE_KEY", "DATABASE_URL"]}}

Use when: Quick secret check, custom pattern searches, scanning specific directories.

### 4. dependency_audit - Dependency Vulnerability Checker
Checks package dependencies for known CVEs across multiple languages.

Parameters:
- type (required): "npm", "pip", "go", "cargo", "composer"
- path: Subdirectory containing the project (default: current dir)
- json: true/false - JSON output (npm only)
- format: Output format (pip-audit only)

Examples:
{"tool": "dependency_audit", "params": {"type": "npm"}}
{"tool": "dependency_audit", "params": {"type": "npm", "json": true}}
{"tool": "dependency_audit", "params": {"type": "go"}}
{"tool": "dependency_audit", "params": {"type": "pip", "format": "json"}}
{"tool": "dependency_audit", "params": {"type": "cargo"}}
{"tool": "dependency_audit", "params": {"type": "composer", "path": "backend/"}}

Use when: Checking for vulnerable packages, CI/CD security gates, dependency upgrades.

### 5. sast_scan - Static Application Security Testing
Runs SAST analysis using Semgrep to find security vulnerabilities in code.

Parameters:
- path: Directory to scan (default: ".")
- config: Ruleset - "auto" (default, detects language), "p/security-audit", "p/owasp-top-ten"
- format: Output format - "text", "json", "sarif"
- severity: Filter - "INFO", "WARNING", "ERROR"

Examples:
{"tool": "sast_scan", "params": {"path": "."}}
{"tool": "sast_scan", "params": {"path": "src/", "config": "p/security-audit"}}
{"tool": "sast_scan", "params": {"path": ".", "config": "p/owasp-top-ten", "severity": "ERROR"}}

Use when: Code review, PR checks, finding injection vulnerabilities, OWASP compliance.

### 6. tfsec_scan - Terraform Security Scanner
Scans Terraform code for security misconfigurations and best practice violations.

Parameters:
- path: Directory with Terraform files (default: ".")
- format: "default", "json", "csv", "checkstyle", "junit", "sarif"
- minimum_severity: "CRITICAL", "HIGH", "MEDIUM", "LOW"
- exclude: Comma-separated check IDs to skip

Examples:
{"tool": "tfsec_scan", "params": {"path": "."}}
{"tool": "tfsec_scan", "params": {"path": "terraform/", "format": "json"}}
{"tool": "tfsec_scan", "params": {"path": ".", "minimum_severity": "HIGH"}}

Use when: Before terraform apply, infrastructure code review, compliance checks.

### 7. kubesec_scan - Kubernetes Manifest Security Scanner
Analyzes Kubernetes YAML manifests for security risks and hardening issues.

Parameters:
- file (required): Path to Kubernetes manifest YAML
- format: "json" or "template"

Examples:
{"tool": "kubesec_scan", "params": {"file": "deployment.yaml"}}
{"tool": "kubesec_scan", "params": {"file": "k8s/prod-deployment.yaml", "format": "json"}}

Use when: Reviewing K8s manifests, before kubectl apply, pod security compliance.

## TOOL SELECTION DECISION TREE

User wants to... → Use this tool:

"Scan Docker image for vulnerabilities" → trivy_scan (type: image)
"Check this image for CVEs" → trivy_scan (type: image)
"Scan codebase for vulnerabilities" → trivy_scan (type: fs)
"Find secrets in the repo" → gitleaks_scan
"Check for hardcoded passwords" → gitleaks_scan or secrets_scan
"Check for leaked API keys" → gitleaks_scan
"Audit npm dependencies" → dependency_audit (type: npm)
"Check for vulnerable packages" → dependency_audit (appropriate type)
"Run govulncheck" → dependency_audit (type: go)
"Find security bugs in code" → sast_scan
"Run SAST analysis" → sast_scan
"Check OWASP vulnerabilities" → sast_scan (config: p/owasp-top-ten)
"Scan Terraform for issues" → tfsec_scan
"Check Terraform security" → tfsec_scan
"Review K8s manifest security" → kubesec_scan
"Check pod security" → kubesec_scan

## FILE TOOLS

{"tool": "read_file", "params": {"file_path": "path"}}
{"tool": "write_file", "params": {"file_path": "path", "content": "..."}}
{"tool": "edit_file", "params": {"file_path": "path", "old_string": "find", "new_string": "replace"}}
{"tool": "list_files", "params": {"directory": ".", "recursive": false}}
{"tool": "find_files", "params": {"pattern": "*.yaml", "directory": "."}}
{"tool": "search_files", "params": {"pattern": "term", "directory": "."}}
{"tool": "execute_command", "params": {"command": "make build"}}

## GIT TOOLS

{"tool": "git_status", "params": {}}
{"tool": "git_diff", "params": {}}
{"tool": "git_log", "params": {"limit": 10}}

## KUBERNETES TOOLS

{"tool": "kubectl_get", "params": {"resource": "pods", "namespace": "default"}}
{"tool": "kubectl_describe", "params": {"resource": "pod", "name": "nginx-xxx", "namespace": "default"}}
{"tool": "kubectl_logs", "params": {"pod": "nginx-xxx", "tail": 100}}

## TERRAFORM TOOLS

{"tool": "terraform_plan", "params": {"out": "plan.tfplan"}}
{"tool": "terraform_output", "params": {"name": "cluster_endpoint"}}
{"tool": "terraform_state", "params": {"subcommand": "list"}}

## DOCKER TOOLS

{"tool": "docker_ps", "params": {"all": true}}
{"tool": "docker_logs", "params": {"container": "myapp", "tail": 100}}

## SECURITY-FIRST GUIDANCE

When analyzing code or configurations:
1. Always check for hardcoded secrets first (use gitleaks_scan)
2. Identify potential injection vulnerabilities (use sast_scan)
3. Review permissions and access controls
4. Check for insecure defaults
5. Recommend security best practices

For vulnerability scanning workflow:
1. trivy_scan on container images BEFORE deployment
2. dependency_audit to check for CVEs in packages
3. sast_scan for code-level security issues
4. tfsec_scan BEFORE terraform apply
5. kubesec_scan BEFORE kubectl apply
6. gitleaks_scan BEFORE any commit

NEVER output actual secret values - always redact them!`

// detectModel queries the /v1/models endpoint to get the served model
func detectModel(ctx gocontext.Context, httpClient *http.Client, host, apiKey string) (string, error) {
	return withRetry(ctx, "model detection", func() (string, error) {
		host = strings.TrimSuffix(host, "/")
		url := host + "/v1/models"

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return "", err // Let isRetryable check this
		}
		defer resp.Body.Close()

		// 5xx errors are retryable (server overloaded)
		if resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("server error (status %d): %s", resp.StatusCode, string(body))
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("failed to query /v1/models (status %d): %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		var modelsResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &modelsResp); err != nil {
			return "", fmt.Errorf("failed to parse /v1/models response: %w", err)
		}

		if len(modelsResp.Data) == 0 {
			return "", fmt.Errorf("no models returned from /v1/models")
		}

		// Return the first (typically only) model
		return modelsResp.Data[0].ID, nil
	})
}

func New(host, apiKey, configModel, vendor string, streaming bool, enableSpinner bool) (*Assistant, error) {
	renderer := ui.NewRenderer()

	// Create context with timeout for provider initialization
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), providerInitTimeout)
	defer cancel()

	// Create provider (auto-detects vendor if not specified)
	prov, err := provider.New(ctx, host, vendor, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	// Get OpenAI-compatible client from provider
	client := prov.CreateClient()

	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Initialize storage manager first (to load persisted model preference)
	var storageMgr *storage.Manager
	var session *storage.Session
	var projectCtx *context.ProjectContext
	var persistedModel string
	var permMgr *permissions.Manager

	storageMgr, err = storage.NewManager(workingDir)
	if err != nil {
		// Storage initialization failed - continue without persistence
		fmt.Println(renderer.WarningMessage(fmt.Sprintf("Could not initialize storage: %v", err)))
	} else {
		// Load persisted model preference
		persistedModel = storageMgr.GetPreferredModel()

		// Try to load or create active session
		session, _ = storageMgr.GetActiveSession()
		if session == nil {
			session, _ = storageMgr.CreateSession("")
		}

		// Load project context if available
		projectCtx, _ = storageMgr.LoadProjectContext()

		// Initialize permission manager
		permMgr, _ = permissions.NewManager(workingDir)
	}

	// Determine which model to use (priority: persisted > config > auto-detect)
	models, err := prov.DetectModels(ctx)
	var model string

	// Helper to check if model exists in available models
	modelAvailable := func(name string) bool {
		for _, m := range models {
			if m == name {
				return true
			}
		}
		return false
	}

	if err != nil {
		// Can't detect models - use persisted or config
		if persistedModel != "" {
			model = persistedModel
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using saved model: %s", model)))
		} else if configModel != "" {
			model = configModel
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Could not detect models (%v), using configured: %s", err, model)))
		} else {
			return nil, fmt.Errorf("failed to detect models and no fallback configured: %w", err)
		}
	} else if len(models) > 0 {
		// Models available - check persisted, then config, then first available
		if persistedModel != "" && modelAvailable(persistedModel) {
			model = persistedModel
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using saved model: %s", model)))
		} else if persistedModel != "" && !modelAvailable(persistedModel) {
			// Saved model no longer available - warn and use first available
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Saved model '%s' not available. Use /model to select.", persistedModel)))
			model = models[0]
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using: %s", model)))
		} else if configModel != "" && modelAvailable(configModel) {
			model = configModel
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using configured model: %s", model)))
		} else if configModel != "" {
			model = models[0]
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Configured model '%s' not available. Using: %s", configModel, model)))
		} else {
			model = models[0]
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using model: %s", model)))
		}
	} else {
		if persistedModel != "" {
			model = persistedModel
		} else if configModel != "" {
			model = configModel
		} else {
			return nil, fmt.Errorf("no models available and no fallback configured")
		}
	}

	// Update provider with selected model
	prov.SetModel(model)

	// Build system prompt - use compact version since we'll try native function calling first
	// If model doesn't support native tools, we'll rebuild with full prompt on fallback
	systemPrompt := buildSystemPromptWithModeAndTools(workingDir, storageMgr, storage.ModeDevOps, true)

	systemMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}

	return &Assistant{
		provider:       prov,
		client:         client,
		model:          model,
		conversation:   []openai.ChatCompletionMessage{systemMessage},
		toolRegistry:   tools.NewRegistry(),
		toolDefs:       tools.GetToolDefinitions(), // Initialize OpenAI function calling tools
		workingDir:     workingDir,
		streaming:      streaming,
		enableSpinner:  enableSpinner,
		renderer:       renderer,
		storage:        storageMgr,
		session:        session,
		projectCtx:     projectCtx,
		sessionUsage:   &storage.TokenUsage{},
		useNativeTools: true, // Start with native tools enabled
		permMgr:        permMgr,
	}, nil
}

// GetSession returns the current session
func (a *Assistant) GetSession() *storage.Session {
	return a.session
}

// GetSessionFresh returns the current session refreshed from storage
// This ensures the message count and other fields are up-to-date
func (a *Assistant) GetSessionFresh() *storage.Session {
	if a.storage == nil || a.session == nil {
		return a.session
	}
	// Reload from storage to get current message count
	session, err := a.storage.GetSession(a.session.ID)
	if err == nil {
		a.session = session
	}
	return a.session
}

// GetStorage returns the storage manager
func (a *Assistant) GetStorage() *storage.Manager {
	return a.storage
}

// GetToolRegistry returns the tool registry
func (a *Assistant) GetToolRegistry() *tools.Registry {
	return a.toolRegistry
}

// GetProvider returns the LLM provider
func (a *Assistant) GetProvider() provider.Provider {
	return a.provider
}

// AddMCPToolDefinition adds an MCP tool definition for LLM function calling
func (a *Assistant) AddMCPToolDefinition(tool openai.Tool) {
	a.toolDefs = append(a.toolDefs, tool)
}

// RemoveMCPToolDefinitions removes all MCP tool definitions for a server
func (a *Assistant) RemoveMCPToolDefinitions(serverName string) {
	filtered := make([]openai.Tool, 0, len(a.toolDefs))
	prefix := serverName + "."
	for _, tool := range a.toolDefs {
		if tool.Function != nil && !strings.HasPrefix(tool.Function.Name, prefix) {
			filtered = append(filtered, tool)
		}
	}
	a.toolDefs = filtered
}

// ClearAuditLog clears the audit log for the current session
func (a *Assistant) ClearAuditLog() error {
	if a.storage == nil || a.session == nil {
		return fmt.Errorf("no active session")
	}
	return a.storage.ClearAuditLog(a.session.ID)
}

// GetUsage returns the current session token usage
func (a *Assistant) GetUsage() *storage.TokenUsage {
	return a.sessionUsage
}

// GetLastResponse returns the last AI response text (for suggestion detection)
func (a *Assistant) GetLastResponse() string {
	return a.lastResponse
}

// GetProviderInfo returns information about the current LLM provider
func (a *Assistant) GetProviderInfo() *provider.Info {
	if a.provider == nil {
		return nil
	}
	return a.provider.Info()
}

// GetCurrentModel returns the current model name
func (a *Assistant) GetCurrentModel() string {
	return a.model
}

// GetMode returns the current operating mode
func (a *Assistant) GetMode() storage.OperatingMode {
	if a.mode == "" {
		return storage.ModeDevOps
	}
	return a.mode
}

// GetProjectContext returns the project context if loaded
func (a *Assistant) GetProjectContext() *context.ProjectContext {
	return a.projectCtx
}

// SetMode switches the operating mode and rebuilds the system prompt
func (a *Assistant) SetMode(mode string) error {
	targetMode := storage.OperatingMode(mode)

	switch targetMode {
	case storage.ModeDevOps:
		a.mode = targetMode
		a.systemPrompt = buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, a.useNativeTools)
		// Update system message in conversation
		if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
			a.conversation[0].Content = a.systemPrompt
		}
		return nil
	case storage.ModeSecurity:
		a.mode = targetMode
		a.systemPrompt = buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, a.useNativeTools)
		// Update system message in conversation
		if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
			a.conversation[0].Content = a.systemPrompt
		}
		// Initialize audit log for security mode
		if a.storage != nil && a.session != nil {
			_ = a.storage.InitAuditLog(a.session.ID, string(storage.ModeSecurity))
		}
		return nil
	default:
		return fmt.Errorf("invalid mode: %s (valid: devops, security)", mode)
	}
}

// ListModels returns available models from the provider (Ollama only)
func (a *Assistant) ListModels() ([]provider.ModelInfo, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Check if provider supports model management
	mm, ok := a.provider.(provider.ModelManager)
	if !ok {
		return nil, fmt.Errorf("provider does not support model listing (Ollama only)")
	}

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), modelOperationTimeout)
	defer cancel()

	return mm.ListModels(ctx)
}

// SwitchModel switches to a different model, unloading the current one first
func (a *Assistant) SwitchModel(newModel string) error {
	if a.provider == nil {
		return fmt.Errorf("provider not initialized")
	}

	// Check if provider supports model management
	mm, ok := a.provider.(provider.ModelManager)
	if !ok {
		return fmt.Errorf("provider does not support model switching (Ollama only)")
	}

	oldModel := a.model

	// Only unload if we're actually changing models
	if oldModel != "" && oldModel != newModel {
		ctx, cancel := gocontext.WithTimeout(gocontext.Background(), modelOperationTimeout)
		defer cancel()

		// Unload the old model to free memory
		if err := mm.UnloadModel(ctx, oldModel); err != nil {
			// Log but don't fail - the model might not be loaded
			fmt.Printf("  Note: Could not unload %s (may not be loaded)\n", oldModel)
		}
	}

	// Set the new model
	a.model = newModel
	a.provider.SetModel(newModel)

	// Persist the model selection
	if a.storage != nil {
		if err := a.storage.SetPreferredModel(newModel); err != nil {
			// Log but don't fail
			fmt.Printf("  Note: Could not save model preference: %v\n", err)
		}
	}

	return nil
}

// ListSessions returns all available sessions
func (a *Assistant) ListSessions() ([]storage.SessionMetadata, error) {
	if a.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	return a.storage.ListSessions()
}

// NewSession creates a new conversation session
func (a *Assistant) NewSession(name string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	session, err := a.storage.CreateSession(name)
	if err != nil {
		return err
	}

	a.session = session

	// Reset conversation to just system message
	systemPrompt := buildSystemPrompt(a.workingDir, a.storage)
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	return nil
}

// LoadSession loads a previous session by ID
func (a *Assistant) LoadSession(id string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	session, err := a.storage.GetSession(id)
	if err != nil {
		return err
	}

	a.session = session
	a.storage.SetActiveSession(id)

	// Rebuild conversation from session messages
	systemPrompt := buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, a.useNativeTools)
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	// Add messages from session, properly restoring tool calls
	for _, msg := range session.Messages {
		switch msg.Role {
		case "user":
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: msg.Content,
			})

		case "assistant":
			assistantMsg := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: msg.Content,
			}

			// Restore tool calls if present (native function calling)
			if len(msg.ToolCalls) > 0 {
				assistantMsg.ToolCalls = convertToOpenAIToolCalls(msg.ToolCalls)
			}

			a.conversation = append(a.conversation, assistantMsg)

		case "tool":
			// Tool response message (native function calling)
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
			})

		case "system":
			// Skip system messages as we've already added our own
			continue
		}
	}

	return nil
}

// convertToOpenAIToolCalls converts storage tool call records to OpenAI format
func convertToOpenAIToolCalls(records []storage.ToolCallRecord) []openai.ToolCall {
	var toolCalls []openai.ToolCall
	for _, rec := range records {
		// Marshal params to JSON for the Arguments field
		argsJSON, _ := json.Marshal(rec.Params)

		toolCalls = append(toolCalls, openai.ToolCall{
			ID:   rec.ID,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      rec.Tool,
				Arguments: string(argsJSON),
			},
		})
	}
	return toolCalls
}

// GenerateSummary creates an AI-generated summary of the current session
// Returns the summary string, or an error if generation fails.
// This is designed to be called on session exit and should not block for too long.
func (a *Assistant) GenerateSummary() (string, error) {
	if a.session == nil || len(a.session.Messages) < 2 {
		return "", nil // Not enough messages to summarize
	}

	// If already has a summary, skip
	if a.session.Summary != "" {
		return a.session.Summary, nil
	}

	// Build a condensed version of the conversation for summarization
	var conversationText strings.Builder
	for _, msg := range a.session.Messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			// Truncate long messages to keep context manageable
			content := msg.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			conversationText.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, content))
		}
	}

	// Create a short timeout context for the summary call
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 30*time.Second)
	defer cancel()

	// Build the summarization request
	summaryRequest := openai.ChatCompletionRequest{
		Model: a.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a helpful assistant. Summarize the following DevOps conversation in 1-2 sentences. Focus on what was accomplished or discussed. Be concise.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: conversationText.String(),
			},
		},
		MaxTokens: 100,
	}

	resp, err := a.client.CreateChatCompletion(ctx, summaryRequest)
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no summary generated")
	}

	// Track usage from summary generation - use API response if available, otherwise estimate
	if resp.Usage.TotalTokens > 0 {
		a.sessionUsage.PromptTokens += resp.Usage.PromptTokens
		a.sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
		a.sessionUsage.TotalTokens += resp.Usage.TotalTokens
	} else {
		// Estimate tokens when API doesn't return usage (common with Ollama)
		// System prompt + conversation text, ~4 chars per token
		promptTokens := (150 + conversationText.Len()) / 4 // 150 chars for system prompt
		completionTokens := len(resp.Choices[0].Message.Content) / 4
		totalTokens := promptTokens + completionTokens

		a.sessionUsage.PromptTokens += promptTokens
		a.sessionUsage.CompletionTokens += completionTokens
		a.sessionUsage.TotalTokens += totalTokens
	}

	summary := strings.TrimSpace(resp.Choices[0].Message.Content)

	// Save the summary to storage
	if a.storage != nil && a.session != nil {
		a.storage.UpdateSessionSummary(a.session.ID, summary)
		a.session.Summary = summary
	}

	return summary, nil
}

// buildSystemPrompt creates the system prompt, including project context if available
func buildSystemPrompt(workingDir string, storageMgr *storage.Manager) string {
	return buildSystemPromptWithModeAndTools(workingDir, storageMgr, storage.ModeDevOps, false)
}

// buildSystemPromptWithMode creates the system prompt with the specified mode
func buildSystemPromptWithMode(workingDir string, storageMgr *storage.Manager, mode storage.OperatingMode) string {
	return buildSystemPromptWithModeAndTools(workingDir, storageMgr, mode, false)
}

// buildSystemPromptWithModeAndTools creates the system prompt with mode and native tools flag
// When useNativeTools is true, uses compact prompts (tool definitions come from API tools parameter)
// When useNativeTools is false, uses full prompts with JSON-in-content tool examples
func buildSystemPromptWithModeAndTools(workingDir string, storageMgr *storage.Manager, mode storage.OperatingMode, useNativeTools bool) string {
	var prompt string
	if mode == storage.ModeSecurity {
		if useNativeTools {
			prompt = securitySystemPromptCompact
		} else {
			prompt = securitySystemPrompt
		}
	} else {
		if useNativeTools {
			prompt = baseSystemPromptCompact
		} else {
			prompt = baseSystemPrompt
		}
	}

	// Check for TARACODE.md in current directory
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	if content, err := os.ReadFile(taracodeFile); err == nil {
		prompt += fmt.Sprintf("\n\n## PROJECT CONTEXT\nThe following is project-specific guidance from TARACODE.md:\n\n%s", string(content))
	}

	// Include relevant project memories if available
	if viper.GetBool("memory.enabled") {
		if memoryMgr := getMemoryManager(workingDir); memoryMgr != nil {
			maxTokens := viper.GetInt("memory.max_context_tokens")
			if maxTokens <= 0 {
				maxTokens = 2000
			}
			memories := memoryMgr.GetRelevantMemories("", maxTokens)
			if len(memories) > 0 {
				prompt += "\n\n## PROJECT MEMORIES\nRemembered facts about this project:\n\n"
				for _, mem := range memories {
					prompt += fmt.Sprintf("- [%s] %s\n", mem.Category, mem.Content)
					// Increment use count asynchronously to avoid blocking
					go memoryMgr.IncrementUseCount(mem.ID)
				}
			}
		}
	}

	// Include active plan if exists
	if storageMgr != nil {
		if plan, err := storageMgr.GetActivePlan(); err == nil && plan != nil {
			prompt += "\n\n## ACTIVE PLAN\n"
			prompt += fmt.Sprintf("**%s**\n", plan.Title)
			for i, task := range plan.Tasks {
				status := "[ ]"
				if task.Status == storage.TaskStatusCompleted {
					status = "[x]"
				} else if task.Status == storage.TaskStatusInProgress {
					status = "[>]"
				}
				prompt += fmt.Sprintf("%d. %s %s\n", i+1, status, task.Content)
			}
			prompt += "\nUpdate task status as you complete them."
		}
	}

	// Add working directory context
	prompt += fmt.Sprintf("\n\nCurrent working directory: %s", workingDir)

	return prompt
}

// getMemoryManager creates a memory manager for the given working directory
// Returns nil if the project is not initialized or memory is not available
func getMemoryManager(workingDir string) *memory.Manager {
	taracodeDir := filepath.Join(workingDir, ".taracode")
	if _, err := os.Stat(taracodeDir); os.IsNotExist(err) {
		return nil
	}
	mm, err := memory.NewManager(taracodeDir)
	if err != nil {
		return nil
	}
	return mm
}

// InitProject analyzes the project and creates TARACODE.md with comprehensive context
func InitProject(workingDir string) error {
	fmt.Println("Analyzing project structure...")

	// Initialize storage manager (creates .taracode/ structure)
	storageMgr, err := storage.NewManager(workingDir)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Explore project with smart filtering
	fmt.Println("  Exploring directories...")
	opts := context.DefaultExplorerOptions()
	tree, err := context.ExploreProject(workingDir, opts)
	if err != nil {
		return fmt.Errorf("failed to explore project: %w", err)
	}

	// Analyze important files
	fmt.Println("  Analyzing key files...")
	analyses := context.AnalyzeImportantFiles(workingDir, tree)

	// Build project context
	projectCtx := &context.ProjectContext{
		RootPath:       workingDir,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Structure:      tree,
		ImportantFiles: analyses,
	}

	// Detect project type, frameworks, and relevant tools
	fmt.Println("  Detecting project type...")
	projectInfo := context.DetectProject(workingDir)
	projectCtx.ProjectType = projectInfo.Type
	projectCtx.ModuleName = projectInfo.ModuleName
	projectCtx.Dependencies = projectInfo.Dependencies
	projectCtx.Frameworks = projectInfo.Frameworks
	projectCtx.DetectedTools = projectInfo.DetectedTools

	// Extract build commands and git info
	extractBuildCommands(workingDir, projectCtx)
	extractGitInfo(workingDir, projectCtx)

	// Save context to .taracode/context/project.json
	if err := storageMgr.SaveProjectContext(projectCtx); err != nil {
		fmt.Printf("  Warning: Could not save project context: %v\n", err)
	}

	// Save project config to .taracode/project.json with detected info
	projectConfig := &storage.ProjectConfig{
		ProjectRoot:   workingDir,
		InitializedAt: time.Now(),
		Version:       "0.3.15",
		ProjectType:   projectInfo.Type,
		DetectedTools: projectInfo.DetectedTools,
		Frameworks:    projectInfo.Frameworks,
	}
	if err := storageMgr.SaveProjectConfig(projectConfig); err != nil {
		fmt.Printf("  Warning: Could not save project config: %v\n", err)
	}

	// Generate TARACODE.md from context
	if err := generateTaracodeMD(workingDir, projectCtx); err != nil {
		return fmt.Errorf("failed to generate TARACODE.md: %w", err)
	}

	// Print summary
	printInitSummary(projectCtx)

	return nil
}

// extractBuildCommands extracts build commands from Makefile
func extractBuildCommands(workingDir string, ctx *context.ProjectContext) {
	content, err := os.ReadFile(filepath.Join(workingDir, "Makefile"))
	if err != nil {
		return
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		// Match targets that are not indented and end with :
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, ".") && !strings.HasPrefix(line, " ") {
			target := strings.TrimSuffix(line, ":")
			// Skip targets with special characters or spaces
			if !strings.ContainsAny(target, " \t$%") {
				ctx.BuildCommands = append(ctx.BuildCommands, fmt.Sprintf("make %s", target))
			}
		}
	}
}

// extractGitInfo extracts git repository information
func extractGitInfo(workingDir string, ctx *context.ProjectContext) {
	gitDir := filepath.Join(workingDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return
	}

	ctx.GitInfo = &context.GitInfo{}

	// Get current branch
	if out, err := exec.Command("git", "-C", workingDir, "branch", "--show-current").Output(); err == nil {
		ctx.GitInfo.Branch = strings.TrimSpace(string(out))
	}

	// Get remote URL
	if out, err := exec.Command("git", "-C", workingDir, "remote", "get-url", "origin").Output(); err == nil {
		ctx.GitInfo.RemoteURL = strings.TrimSpace(string(out))
	}

	// Check for uncommitted changes
	if out, err := exec.Command("git", "-C", workingDir, "status", "--porcelain").Output(); err == nil {
		ctx.GitInfo.HasUncommitted = len(strings.TrimSpace(string(out))) > 0
	}

	// Get last commit
	if out, err := exec.Command("git", "-C", workingDir, "log", "-1", "--format=%h %s").Output(); err == nil {
		ctx.GitInfo.LastCommit = strings.TrimSpace(string(out))
	}
}

// generateTaracodeMD creates the TARACODE.md file from project context
func generateTaracodeMD(workingDir string, ctx *context.ProjectContext) error {
	var sb strings.Builder

	sb.WriteString("# TARACODE.md\n\n")
	sb.WriteString("This file provides context to Tara Code. Auto-generated by `/init`.\n\n")

	// Project overview
	sb.WriteString("## Project Overview\n\n")
	if ctx.ProjectType != "" {
		sb.WriteString(fmt.Sprintf("**Type:** %s project\n", ctx.ProjectType))
	}
	if ctx.ModuleName != "" {
		sb.WriteString(fmt.Sprintf("**Module:** %s\n", ctx.ModuleName))
	}
	sb.WriteString("\n")

	// Project structure (tree view)
	sb.WriteString("## Project Structure\n\n```\n")
	writeTreeStructure(&sb, ctx.Structure, "", true)
	sb.WriteString("```\n\n")

	// Important files with summaries
	if len(ctx.ImportantFiles) > 0 {
		sb.WriteString("## Key Files\n\n")
		for _, file := range ctx.ImportantFiles {
			sb.WriteString(fmt.Sprintf("- **`%s`** - %s\n", file.Path, file.Summary))
		}
		sb.WriteString("\n")
	}

	// Build commands
	if len(ctx.BuildCommands) > 0 {
		sb.WriteString("## Build Commands\n\n```bash\n")
		for _, cmd := range ctx.BuildCommands {
			sb.WriteString(cmd + "\n")
		}
		sb.WriteString("```\n\n")
	}

	// Git info
	if ctx.GitInfo != nil && ctx.GitInfo.Branch != "" {
		sb.WriteString("## Git Info\n\n")
		sb.WriteString(fmt.Sprintf("- **Branch:** %s\n", ctx.GitInfo.Branch))
		if ctx.GitInfo.RemoteURL != "" {
			sb.WriteString(fmt.Sprintf("- **Remote:** %s\n", ctx.GitInfo.RemoteURL))
		}
		if ctx.GitInfo.LastCommit != "" {
			sb.WriteString(fmt.Sprintf("- **Last commit:** %s\n", ctx.GitInfo.LastCommit))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n*Edit this file to add custom instructions for Tara Code.*\n")

	return os.WriteFile(filepath.Join(workingDir, "TARACODE.md"), []byte(sb.String()), 0644)
}

// writeTreeStructure writes the directory tree in a visual format
func writeTreeStructure(sb *strings.Builder, node *context.DirectoryTree, prefix string, isLast bool) {
	if node == nil {
		return
	}

	// Handle root node specially
	if node.Path == "" {
		for i, child := range node.Children {
			writeTreeStructure(sb, child, "", i == len(node.Children)-1)
		}
		return
	}

	connector := "├── "
	if isLast {
		connector = "└── "
	}

	displayName := node.Name
	if node.IsDir {
		displayName += "/"
	}

	sb.WriteString(prefix + connector + displayName + "\n")

	if node.IsDir && len(node.Children) > 0 {
		newPrefix := prefix
		if isLast {
			newPrefix += "    "
		} else {
			newPrefix += "│   "
		}

		for i, child := range node.Children {
			writeTreeStructure(sb, child, newPrefix, i == len(node.Children)-1)
		}
	}
}

// printInitSummary prints a summary of the initialization
func printInitSummary(ctx *context.ProjectContext) {
	fmt.Println()
	fmt.Println("✓ Project initialized successfully!")
	fmt.Println()

	if ctx.ProjectType != "" {
		fmt.Printf("  Type: %s", ctx.ProjectType)
		if ctx.ModuleName != "" {
			fmt.Printf(" (%s)", ctx.ModuleName)
		}
		fmt.Println()
	}

	// Show detected frameworks
	if len(ctx.Frameworks) > 0 {
		fmt.Printf("  Frameworks: %s\n", strings.Join(ctx.Frameworks, ", "))
	}

	fileCount := context.CountFiles(ctx.Structure)
	dirCount := context.CountDirs(ctx.Structure)
	fmt.Printf("  Structure: %d files, %d directories\n", fileCount, dirCount)
	fmt.Printf("  Key files analyzed: %d\n", len(ctx.ImportantFiles))

	if len(ctx.BuildCommands) > 0 {
		fmt.Printf("  Build commands: %d\n", len(ctx.BuildCommands))
	}

	if ctx.GitInfo != nil && ctx.GitInfo.Branch != "" {
		fmt.Printf("  Git branch: %s\n", ctx.GitInfo.Branch)
	}

	// Show detected tools
	if len(ctx.DetectedTools) > 0 {
		fmt.Println()
		fmt.Printf("  Detected %d relevant tools for this project:\n", len(ctx.DetectedTools))
		// Show up to 10 tools, then summarize
		maxShow := 10
		for i, tool := range ctx.DetectedTools {
			if i >= maxShow {
				fmt.Printf("    ... and %d more (use /tools to see all)\n", len(ctx.DetectedTools)-maxShow)
				break
			}
			fmt.Printf("    - %s\n", tool)
		}
	}

	fmt.Println()
	fmt.Println("  Created:")
	fmt.Println("    - TARACODE.md (project context for AI)")
	fmt.Println("    - .taracode/ (storage for history, plans, state)")
	fmt.Println()
	fmt.Println("Edit TARACODE.md to add custom instructions.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ToolCall represents a parsed tool call from the model's response
type ToolCall struct {
	ID     string                 `json:"id,omitempty"` // Tool call ID for native function calling
	Tool   string                 `json:"tool"`
	Params map[string]interface{} `json:"params"`
}

// cleanResponse removes thinking tags and extracts displayable content
func cleanResponse(response string) string {
	// Remove <think>...</think> blocks (DeepSeek R1 reasoning)
	thinkRe := regexp.MustCompile(`(?s)<think>.*?</think>`)
	cleaned := thinkRe.ReplaceAllString(response, "")

	// Also handle unclosed think tags
	if idx := strings.Index(cleaned, "</think>"); idx != -1 {
		cleaned = cleaned[idx+8:]
	}

	return strings.TrimSpace(cleaned)
}

// normalizeJSON cleans up JSON that may have been corrupted by model text wrapping
// It removes extra whitespace and newlines that aren't part of actual string content
func normalizeJSON(jsonStr string) string {
	// Remove carriage returns
	result := strings.ReplaceAll(jsonStr, "\r", "")

	// Process character by character to handle strings properly
	var normalized strings.Builder
	inString := false
	escaped := false

	for i := 0; i < len(result); i++ {
		c := result[i]

		if escaped {
			normalized.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' && inString {
			normalized.WriteByte(c)
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			normalized.WriteByte(c)
			continue
		}

		if inString {
			// Inside a string - convert actual newlines to \n escape sequence
			if c == '\n' {
				normalized.WriteString("\\n")
			} else if c == '\t' {
				normalized.WriteString("\\t")
			} else {
				normalized.WriteByte(c)
			}
		} else {
			// Outside string - skip whitespace except single spaces
			if c == '\n' || c == '\t' {
				// Skip newlines and tabs outside strings
				continue
			} else if c == ' ' {
				// Collapse multiple spaces to single space
				if normalized.Len() > 0 {
					lastChar := normalized.String()[normalized.Len()-1]
					if lastChar != ' ' && lastChar != '{' && lastChar != '[' && lastChar != ':' && lastChar != ',' {
						normalized.WriteByte(c)
					}
				}
			} else {
				normalized.WriteByte(c)
			}
		}
	}

	return normalized.String()
}

// tryConvertToToolCall attempts to convert alternative JSON formats to standard tool call format
// This handles models that output {"file_name": "X", "content": "Y"} instead of proper tool format
func tryConvertToToolCall(jsonStr string) *ToolCall {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil
	}

	// Already has "tool" key - not a malformed call
	if _, hasToolKey := raw["tool"]; hasToolKey {
		return nil
	}

	// Detect write_file pattern: has "content" and some file path key
	if content, hasContent := raw["content"]; hasContent {
		var filePath string
		for _, key := range []string{"file_path", "file_name", "filename", "path", "name"} {
			if fp, ok := raw[key].(string); ok && fp != "" {
				filePath = fp
				break
			}
		}
		if filePath != "" {
			return &ToolCall{
				Tool: "write_file",
				Params: map[string]interface{}{
					"file_path": filePath,
					"content":   content,
				},
			}
		}
	}

	// Detect read_file pattern: has file path but no content
	for _, key := range []string{"file_path", "file_name", "filename", "path", "file"} {
		if fp, ok := raw[key].(string); ok && fp != "" {
			// Check if this looks like a read operation (no content, or has "read" action)
			if _, hasContent := raw["content"]; !hasContent {
				if action, ok := raw["action"].(string); ok {
					if action == "read" || action == "open" || action == "get" {
						return &ToolCall{
							Tool:   "read_file",
							Params: map[string]interface{}{"file_path": fp},
						}
					}
				}
			}
			break
		}
	}

	// Detect command execution pattern
	for _, key := range []string{"command", "cmd", "shell", "exec"} {
		if cmd, ok := raw[key].(string); ok && cmd != "" {
			return &ToolCall{
				Tool:   "execute_command",
				Params: map[string]interface{}{"command": cmd},
			}
		}
	}

	return nil
}

// extractJSONObjects finds JSON objects using brace matching
// If strictToolFormat is true, only finds objects starting with {"tool"
// If false, finds any JSON object (for fallback conversion)
func extractJSONObjects(text string) []string {
	return extractJSONObjectsWithPattern(text, `\{\s*"tool"\s*:`)
}

// extractAllJSONObjects finds any JSON object in text (for fallback parsing)
func extractAllJSONObjects(text string) []string {
	return extractJSONObjectsWithPattern(text, `\{`)
}

func extractJSONObjectsWithPattern(text string, pattern string) []string {
	var results []string

	// Find all positions where a JSON object might start
	re := regexp.MustCompile(pattern)
	indices := re.FindAllStringIndex(text, -1)

	for _, idx := range indices {
		start := idx[0]
		depth := 0
		inString := false
		escaped := false
		end := -1

		for i := start; i < len(text); i++ {
			c := text[i]

			if escaped {
				escaped = false
				continue
			}

			if c == '\\' && inString {
				escaped = true
				continue
			}

			if c == '"' {
				inString = !inString
				continue
			}

			if !inString {
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						end = i + 1
						break
					}
				}
			}
		}

		if end > start {
			jsonStr := text[start:end]
			results = append(results, jsonStr)
		}
	}

	return results
}

// parseToolCalls extracts ALL tool calls from the model's response (supports multiple tools)
func parseToolCalls(response string) ([]*ToolCall, string) {
	cleaned := cleanResponse(response)
	var toolCalls []*ToolCall
	seen := make(map[string]bool) // Track seen tool calls to avoid duplicates
	var firstToolIdx int = -1

	// Extract JSON objects using brace matching (handles nested objects and multiline)
	jsonObjects := extractJSONObjects(cleaned)

	for _, jsonStr := range jsonObjects {
		// Normalize the JSON to fix text-wrapping artifacts
		normalized := normalizeJSON(jsonStr)

		// Try to unmarshal
		var toolCall ToolCall
		if err := json.Unmarshal([]byte(normalized), &toolCall); err == nil {
			if toolCall.Tool != "" {
				// Create a key to track duplicates
				key := toolCall.Tool + ":" + fmt.Sprintf("%v", toolCall.Params)
				if !seen[key] {
					seen[key] = true
					toolCalls = append(toolCalls, &toolCall)

					// Track position of first tool call
					if firstToolIdx == -1 {
						firstToolIdx = strings.Index(cleaned, jsonStr)
					}
				}
			}
		}
	}

	// Also try to find JSON arrays of tool calls
	// [{"tool": "...", "params": {...}}, {"tool": "...", "params": {...}}]
	arrayPattern := regexp.MustCompile(`\[\s*\{`)
	if arrayIdx := arrayPattern.FindStringIndex(cleaned); arrayIdx != nil {
		// Find matching closing bracket
		start := arrayIdx[0]
		depth := 0
		inString := false
		escaped := false
		end := -1

		for i := start; i < len(cleaned); i++ {
			c := cleaned[i]
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && inString {
				escaped = true
				continue
			}
			if c == '"' {
				inString = !inString
				continue
			}
			if !inString {
				if c == '[' {
					depth++
				} else if c == ']' {
					depth--
					if depth == 0 {
						end = i + 1
						break
					}
				}
			}
		}

		if end > start {
			arrayStr := normalizeJSON(cleaned[start:end])
			var arrayToolCalls []ToolCall
			if err := json.Unmarshal([]byte(arrayStr), &arrayToolCalls); err == nil {
				for i := range arrayToolCalls {
					if arrayToolCalls[i].Tool != "" {
						key := arrayToolCalls[i].Tool + ":" + fmt.Sprintf("%v", arrayToolCalls[i].Params)
						if !seen[key] {
							seen[key] = true
							toolCalls = append(toolCalls, &arrayToolCalls[i])
						}
					}
				}
				if firstToolIdx == -1 || start < firstToolIdx {
					firstToolIdx = start
				}
			}
		}
	}

	// Fallback: if no standard tool calls found, try to convert alternative JSON formats
	if len(toolCalls) == 0 {
		allJsonObjects := extractAllJSONObjects(cleaned)
		for _, jsonStr := range allJsonObjects {
			normalized := normalizeJSON(jsonStr)
			if converted := tryConvertToToolCall(normalized); converted != nil {
				key := converted.Tool + ":" + fmt.Sprintf("%v", converted.Params)
				if !seen[key] {
					seen[key] = true
					toolCalls = append(toolCalls, converted)
					if firstToolIdx == -1 {
						firstToolIdx = strings.Index(cleaned, jsonStr)
					}
				}
			}
		}
	}

	// Extract text before first tool call for display
	textBefore := cleaned
	if len(toolCalls) > 0 && firstToolIdx > 0 {
		textBefore = strings.TrimSpace(cleaned[:firstToolIdx])
	} else if len(toolCalls) > 0 {
		textBefore = ""
	}

	return toolCalls, textBefore
}

// formatToolStatus returns a concise, human-friendly status for tool execution
func formatToolStatus(tool string, params map[string]interface{}, result string, isError bool) string {
	gray := "\033[90m"
	green := "\033[32m"
	red := "\033[31m"
	reset := "\033[0m"

	if isError {
		return fmt.Sprintf("%s✗ %s failed%s", red, tool, reset)
	}

	switch tool {
	case "read_file":
		filePath, _ := params["file_path"].(string)
		lines := strings.Count(result, "\n") + 1
		return fmt.Sprintf("%s→ Read %s (%d lines)%s", gray, filepath.Base(filePath), lines, reset)

	case "search_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No matches") {
			return fmt.Sprintf("%s→ Searched for \"%s\" (no matches)%s", gray, pattern, reset)
		}
		return fmt.Sprintf("%s→ Searched for \"%s\" (%d matches)%s", gray, pattern, matches, reset)

	case "list_files":
		dir, _ := params["directory"].(string)
		if dir == "" || dir == "." {
			dir = "current directory"
		}
		items := strings.Count(result, "\n")
		return fmt.Sprintf("%s→ Listed %s (%d items)%s", gray, dir, items, reset)

	case "execute_command":
		cmd, _ := params["command"].(string)
		if len(cmd) > ui.MaxCommandDisplay {
			cmd = cmd[:ui.MaxCommandDisplay-3] + "..."
		}
		return fmt.Sprintf("%s→ Executed: %s%s", gray, cmd, reset)

	case "write_file":
		filePath, _ := params["file_path"].(string)
		return fmt.Sprintf("%s✓ Wrote %s%s", green, filepath.Base(filePath), reset)

	case "append_file":
		filePath, _ := params["file_path"].(string)
		return fmt.Sprintf("%s✓ Appended to %s%s", green, filepath.Base(filePath), reset)

	case "edit_file":
		filePath, _ := params["file_path"].(string)
		return fmt.Sprintf("%s✓ Edited %s%s", green, filepath.Base(filePath), reset)

	case "insert_lines":
		filePath, _ := params["file_path"].(string)
		lineNum, _ := params["line_number"].(float64)
		return fmt.Sprintf("%s✓ Inserted at line %d in %s%s", green, int(lineNum), filepath.Base(filePath), reset)

	case "replace_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return fmt.Sprintf("%s✓ Replaced lines %d-%d in %s%s", green, int(startLine), int(endLine), filepath.Base(filePath), reset)

	case "delete_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return fmt.Sprintf("%s✓ Deleted lines %d-%d from %s%s", green, int(startLine), int(endLine), filepath.Base(filePath), reset)

	case "copy_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return fmt.Sprintf("%s✓ Copied %s to %s%s", green, filepath.Base(src), filepath.Base(dst), reset)

	case "move_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return fmt.Sprintf("%s✓ Moved %s to %s%s", green, filepath.Base(src), filepath.Base(dst), reset)

	case "delete_file":
		filePath, _ := params["file_path"].(string)
		recursive, _ := params["recursive"].(bool)
		if recursive {
			return fmt.Sprintf("%s✓ Deleted %s (recursive)%s", green, filepath.Base(filePath), reset)
		}
		return fmt.Sprintf("%s✓ Deleted %s%s", green, filepath.Base(filePath), reset)

	case "create_directory":
		dirPath, _ := params["path"].(string)
		return fmt.Sprintf("%s✓ Created directory %s%s", green, filepath.Base(dirPath), reset)

	case "find_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No files found") {
			return fmt.Sprintf("%s→ Find \"%s\" (no matches)%s", gray, pattern, reset)
		}
		return fmt.Sprintf("%s→ Find \"%s\" (%d files)%s", gray, pattern, matches, reset)

	case "git_status":
		if strings.Contains(result, "clean") {
			return fmt.Sprintf("%s→ Git status: clean%s", gray, reset)
		}
		changes := strings.Count(result, "\n")
		return fmt.Sprintf("%s→ Git status: %d changes%s", gray, changes, reset)

	case "git_diff":
		if strings.Contains(result, "No changes") {
			return fmt.Sprintf("%s→ Git diff: no changes%s", gray, reset)
		}
		lines := strings.Count(result, "\n")
		return fmt.Sprintf("%s→ Git diff: %d lines%s", gray, lines, reset)

	case "git_log":
		commits := strings.Count(result, "\n") + 1
		return fmt.Sprintf("%s→ Git log: %d commits%s", gray, commits, reset)

	case "git_add":
		return fmt.Sprintf("%s✓ Git: staged files%s", green, reset)

	case "git_commit":
		return fmt.Sprintf("%s✓ Git: commit created%s", green, reset)

	case "git_branch":
		branches := strings.Count(result, "\n") + 1
		return fmt.Sprintf("%s→ Git branches: %d%s", gray, branches, reset)

	default:
		return fmt.Sprintf("%s→ %s completed%s", gray, tool, reset)
	}
}

func (a *Assistant) ProcessMessage(userMessage string) error {
	return a.ProcessMessageWithImages(userMessage, nil)
}

// ProcessMessageWithImages handles user messages that may include images
func (a *Assistant) ProcessMessageWithImages(userMessage string, images []*ImageData) error {
	if a.streaming {
		return a.processMessageStreamingWithImages(userMessage, images)
	}
	return a.processMessageNonStreamingWithImages(userMessage, images)
}

// buildUserMessage creates an OpenAI message with optional images
func buildUserMessage(text string, images []*ImageData) openai.ChatCompletionMessage {
	if len(images) == 0 {
		return openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: text,
		}
	}

	// Build multipart message with text and images
	parts := make([]openai.ChatMessagePart, 0, len(images)+1)

	// Add text part first
	parts = append(parts, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: text,
	})

	// Add image parts
	for _, img := range images {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL:    img.ToDataURL(),
				Detail: openai.ImageURLDetailAuto,
			},
		})
	}

	return openai.ChatCompletionMessage{
		Role:         openai.ChatMessageRoleUser,
		MultiContent: parts,
	}
}

// processMessageStreamingWithImages handles messages with real-time streaming output (with optional images)
func (a *Assistant) processMessageStreamingWithImages(userMessage string, images []*ImageData) error {
	// Record user message to session
	if a.storage != nil && a.session != nil {
		userMsg := storage.ConversationMessage{
			Role:      "user",
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		a.storage.AddMessage(a.session.ID, userMsg)
	}

	// Build user message with optional images
	a.conversation = append(a.conversation, buildUserMessage(userMessage, images))

	// Create context with timeout for API response
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	for i := 0; i < maxToolIterations; i++ {
		// Start thinking spinner with Claude Code style status line
		var thinkingSpinner *ui.Spinner
		if a.enableSpinner {
			thinkingSpinner = ui.NewStatusLineSpinner()
			thinkingSpinner.UpdateTokens(a.sessionUsage.TotalTokens)
			thinkingSpinner.Start("")
		}

		// Build request - try with tools first, fall back without if not supported
		req := openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: a.conversation,
			Tools:    a.toolDefs, // Add tool definitions for function calling
			StreamOptions: &openai.StreamOptions{
				IncludeUsage: true,
			},
		}

		stream, err := a.client.CreateChatCompletionStream(ctx, req)

		// If model doesn't support tools or there's a tool-related error, retry without them
		if err != nil && (strings.Contains(err.Error(), "does not support tools") ||
			strings.Contains(err.Error(), "tools.function.parameters") ||
			strings.Contains(err.Error(), "400 Bad Request")) {
			req.Tools = nil // Remove tools - fall back to JSON-in-content
			a.useNativeTools = false

			// Rebuild system prompt with full tool examples since we're falling back
			fullPrompt := buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, false)
			if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
				a.conversation[0].Content = fullPrompt
				req.Messages = a.conversation
			}

			stream, err = a.client.CreateChatCompletionStream(ctx, req)
		}

		if err != nil {
			if thinkingSpinner != nil {
				thinkingSpinner.Stop()
			}
			return fmt.Errorf("failed to create stream: %w", err)
		}

		filter := NewStreamFilter()

		// Accumulate tool calls from stream (native function calling)
		var streamToolCalls []openai.ToolCall
		toolCallsMap := make(map[int]*openai.ToolCall) // Index -> ToolCall for accumulation

		// Buffer the response while showing spinner (Claude Code style)
		for {
			chunk, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				if thinkingSpinner != nil {
					thinkingSpinner.Stop()
				}
				stream.Close()
				return fmt.Errorf("stream error: %w", err)
			}

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta

				// Accumulate content
				if delta.Content != "" {
					filter.Process(delta.Content)
				}

				// Accumulate tool calls from delta (OpenAI function calling)
				for _, tc := range delta.ToolCalls {
					idx := 0
					if tc.Index != nil {
						idx = *tc.Index
					}

					if existing, ok := toolCallsMap[idx]; ok {
						// Append to existing tool call
						if tc.Function.Arguments != "" {
							existing.Function.Arguments += tc.Function.Arguments
						}
					} else {
						// New tool call
						newTC := openai.ToolCall{
							ID:   tc.ID,
							Type: tc.Type,
							Function: openai.FunctionCall{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						}
						toolCallsMap[idx] = &newTC
					}
				}
			}

			// Capture usage from final chunk (when StreamOptions.IncludeUsage is true)
			if chunk.Usage != nil {
				a.sessionUsage.PromptTokens += chunk.Usage.PromptTokens
				a.sessionUsage.CompletionTokens += chunk.Usage.CompletionTokens
				a.sessionUsage.TotalTokens += chunk.Usage.TotalTokens
				// Update spinner with new token count
				if thinkingSpinner != nil {
					thinkingSpinner.UpdateTokens(a.sessionUsage.TotalTokens)
				}
			}
		}
		stream.Close()

		// Convert map to slice
		for i := 0; i < len(toolCallsMap); i++ {
			if tc, ok := toolCallsMap[i]; ok {
				streamToolCalls = append(streamToolCalls, *tc)
			}
		}

		// Stop spinner now that response is complete
		if thinkingSpinner != nil {
			thinkingSpinner.Stop()
		}

		// Flush any remaining buffered content
		filter.Flush()

		fullResponse := filter.FullContent()

		// Store last response for suggestion detection
		a.lastResponse = fullResponse

		// Check for native function calls first, then fall back to JSON parsing
		var nativeToolCalls []ToolCall
		hasNativeToolCalls := len(streamToolCalls) > 0

		if hasNativeToolCalls {
			// Convert OpenAI tool calls to our format
			for _, tc := range streamToolCalls {
				var params map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
					params = make(map[string]interface{})
				}
				nativeToolCalls = append(nativeToolCalls, ToolCall{
					ID:     tc.ID,
					Tool:   tc.Function.Name,
					Params: params,
				})
			}
		}

		// Try JSON parsing from content as fallback (backward compatibility)
		jsonToolCallPtrs, displayText := parseToolCalls(fullResponse)

		// Use native tool calls if available, otherwise use JSON-parsed ones
		var toolCalls []ToolCall
		if len(nativeToolCalls) > 0 {
			toolCalls = nativeToolCalls
			displayText = cleanResponse(fullResponse) // Display the full response without JSON
		} else {
			// Convert pointer slice to value slice
			for _, tc := range jsonToolCallPtrs {
				toolCalls = append(toolCalls, *tc)
			}
		}

		if len(toolCalls) == 0 {
			// No tool calls - render the response with Glamour
			displayedText := cleanResponse(fullResponse)
			if displayedText != "" {
				fmt.Println(ui.RenderMarkdown(displayedText))
			}
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: fullResponse,
			})

			// Save assistant response to session
			if a.storage != nil && a.session != nil {
				assistantMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   fullResponse,
					Timestamp: time.Now(),
				}
				a.storage.AddMessage(a.session.ID, assistantMsg)
			}
			break
		}

		// Display any text before tool calls
		if displayText != "" {
			fmt.Println(ui.RenderMarkdown(displayText))
		}

		// Build assistant message with tool calls for conversation
		assistantMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: fullResponse,
		}
		if hasNativeToolCalls {
			assistantMsg.ToolCalls = streamToolCalls
		}
		a.conversation = append(a.conversation, assistantMsg)

		// Save assistant message with tool calls to session (once, before execution)
		if a.storage != nil && a.session != nil {
			var toolCallRecords []storage.ToolCallRecord
			for _, tc := range toolCalls {
				toolCallRecords = append(toolCallRecords, storage.ToolCallRecord{
					ID:     tc.ID,
					Tool:   tc.Tool,
					Params: tc.Params,
				})
			}
			storageAssistantMsg := storage.ConversationMessage{
				Role:      "assistant",
				Content:   fullResponse,
				Timestamp: time.Now(),
				ToolCalls: toolCallRecords,
			}
			a.storage.AddMessage(a.session.ID, storageAssistantMsg)
		}

		// Execute all tool calls
		totalTools := len(toolCalls)
		var toolMessages []openai.ChatCompletionMessage

		// Count tools requiring security audit for batch handling
		var auditBatch *ui.BatchAuditContext
		if a.mode == storage.ModeSecurity {
			auditCount := 0
			for _, tc := range toolCalls {
				cat := permissions.GetToolCategory(tc.Tool)
				if cat != permissions.CategoryRead {
					auditCount++
				}
			}
			if auditCount > 1 {
				auditBatch = &ui.BatchAuditContext{
					TotalTools:   auditCount,
					CurrentIndex: 0,
				}
			}
		}

		for idx, toolCall := range toolCalls {
			var result string
			var isError bool
			var duration int64

			// Check permission before executing
			if allowed, permResult := a.checkToolPermission(toolCall.Tool, toolCall.Params); !allowed {
				result = permResult
				isError = true
				duration = 0
			} else {
				// Update batch index for non-read operations
				if auditBatch != nil {
					cat := permissions.GetToolCategory(toolCall.Tool)
					if cat != permissions.CategoryRead {
						auditBatch.CurrentIndex++
					}
				}

				// Security audit enforcement (security mode only)
				// This is separate from permissions - even "always allow" tools get audited in security mode
				if auditAllowed, auditResult := a.checkSecurityAudit(toolCall.Tool, toolCall.Params, auditBatch); !auditAllowed {
					result = auditResult
					isError = false // User denial is not an error, it's a choice

					// Build tool result message for blocked operation
					if hasNativeToolCalls && toolCall.ID != "" {
						toolMessages = append(toolMessages, openai.ChatCompletionMessage{
							Role:       openai.ChatMessageRoleTool,
							Content:    result,
							ToolCallID: toolCall.ID,
						})
					}

					// Save blocked operation to session
					if a.storage != nil && a.session != nil {
						toolResultMsg := storage.ConversationMessage{
							Role:       "tool",
							Content:    result,
							Timestamp:  time.Now(),
							ToolCallID: toolCall.ID,
							ToolCall: &storage.ToolCallRecord{
								ID:       toolCall.ID,
								Tool:     toolCall.Tool,
								Params:   toolCall.Params,
								Result:   result,
								Duration: 0,
								Success:  false,
							},
						}
						a.storage.AddMessage(a.session.ID, toolResultMsg)
					}
					continue // Skip to next tool
				}

				// Handle edit preview for edit_file operations
				if toolCall.Tool == "edit_file" {
					proceed, previewResult, _ := a.handleEditPreview(toolCall.Params)
					if !proceed {
						result = previewResult
						isError = false // Cancellation is not an error
						duration = 0
						// Skip to next tool - DisplayEditCancelled already printed the message
						continue
					}
				}

				// Start tool execution spinner with progress
				var toolSpinner *ui.Spinner
				if a.enableSpinner {
					toolSpinner = ui.NewSpinner()
					if totalTools > 1 {
						toolSpinner.Start(fmt.Sprintf("Running %s (%d/%d)...", toolCall.Tool, idx+1, totalTools))
					} else {
						toolSpinner.Start(fmt.Sprintf("Running %s...", toolCall.Tool))
					}
				}

				// Inject security defaults for security tools
				execParams := toolCall.Params
				if tools.IsSecurityTool(toolCall.Tool) {
					defaultSeverity := viper.GetString("security.default_severity")
					execParams = tools.InjectSecurityDefaults(toolCall.Tool, toolCall.Params, defaultSeverity)
				}

				// Execute the tool
				startTime := time.Now()
				var err error
				result, err = a.toolRegistry.ExecuteTool(toolCall.Tool, execParams, a.workingDir)
				duration = time.Since(startTime).Milliseconds()
				isError = err != nil
				if isError {
					result = fmt.Sprintf("Error: %v", err)
				}

				// Stop tool spinner
				if toolSpinner != nil {
					toolSpinner.Stop()
				}
			}

			// Print concise tool status using renderer
			fmt.Println(a.renderer.FormatToolStatus(toolCall.Tool, toolCall.Params, result, isError))

			// Build tool result message
			if hasNativeToolCalls && toolCall.ID != "" {
				// Native function calling - use tool message format
				toolMessages = append(toolMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: toolCall.ID,
				})

				// Save tool response to session
				if a.storage != nil && a.session != nil {
					toolResultMsg := storage.ConversationMessage{
						Role:       "tool",
						Content:    result,
						Timestamp:  time.Now(),
						ToolCallID: toolCall.ID,
						ToolCall: &storage.ToolCallRecord{
							ID:       toolCall.ID,
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolResultMsg)
				}
			} else {
				// Fallback - aggregate results as user message
				if len(toolMessages) == 0 {
					toolMessages = append(toolMessages, openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleUser,
						Content: "",
					})
				}
				if totalTools > 1 {
					toolMessages[0].Content += fmt.Sprintf("[%d] %s result:\n%s\n\n", idx+1, toolCall.Tool, result)
				} else {
					toolMessages[0].Content = fmt.Sprintf("Tool result:\n%s", result)
				}

				// Save tool call result for fallback mode (legacy behavior)
				if a.storage != nil && a.session != nil {
					toolMsg := storage.ConversationMessage{
						Role:      "assistant",
						Content:   fullResponse,
						Timestamp: time.Now(),
						ToolCall: &storage.ToolCallRecord{
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolMsg)
				}
			}
		}

		// Add tool result messages to conversation
		a.conversation = append(a.conversation, toolMessages...)
	}

	return nil
}

// processMessageNonStreamingWithImages handles messages without streaming (with optional images)
func (a *Assistant) processMessageNonStreamingWithImages(userMessage string, images []*ImageData) error {
	// Record user message to session
	if a.storage != nil && a.session != nil {
		userMsg := storage.ConversationMessage{
			Role:      "user",
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		a.storage.AddMessage(a.session.ID, userMsg)
	}

	// Build user message with optional images
	a.conversation = append(a.conversation, buildUserMessage(userMessage, images))

	// Create context with timeout for API response
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	for i := 0; i < maxToolIterations; i++ {
		// Start thinking spinner with Claude Code style status line
		var thinkingSpinner *ui.Spinner
		if a.enableSpinner {
			thinkingSpinner = ui.NewStatusLineSpinner()
			thinkingSpinner.UpdateTokens(a.sessionUsage.TotalTokens)
			thinkingSpinner.Start("")
		}

		// Build request - try with tools first, fall back without if not supported
		req := openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: a.conversation,
			Tools:    a.toolDefs, // Add tool definitions for function calling
		}

		resp, err := a.client.CreateChatCompletion(ctx, req)

		// If model doesn't support tools or there's a tool-related error, retry without them
		if err != nil && (strings.Contains(err.Error(), "does not support tools") ||
			strings.Contains(err.Error(), "tools.function.parameters") ||
			strings.Contains(err.Error(), "400 Bad Request")) {
			req.Tools = nil // Remove tools - fall back to JSON-in-content
			a.useNativeTools = false

			// Rebuild system prompt with full tool examples since we're falling back
			fullPrompt := buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, false)
			if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
				a.conversation[0].Content = fullPrompt
				req.Messages = a.conversation
			}

			resp, err = a.client.CreateChatCompletion(ctx, req)
		}

		// Stop spinner
		if thinkingSpinner != nil {
			thinkingSpinner.Stop()
		}

		if err != nil {
			return fmt.Errorf("failed to get response: %w", err)
		}

		if len(resp.Choices) == 0 {
			return fmt.Errorf("no response choices returned")
		}

		// Track token usage
		if resp.Usage.TotalTokens > 0 {
			a.sessionUsage.PromptTokens += resp.Usage.PromptTokens
			a.sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
			a.sessionUsage.TotalTokens += resp.Usage.TotalTokens
		}

		message := resp.Choices[0].Message
		assistantResponse := message.Content

		// Check for native function calls first
		var toolCalls []ToolCall
		hasNativeToolCalls := len(message.ToolCalls) > 0

		if hasNativeToolCalls {
			// Convert OpenAI tool calls to our format
			for _, tc := range message.ToolCalls {
				var params map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
					params = make(map[string]interface{})
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:     tc.ID,
					Tool:   tc.Function.Name,
					Params: params,
				})
			}
		}

		// Fall back to JSON parsing from content (backward compatibility)
		var displayText string
		if len(toolCalls) == 0 {
			jsonToolCallPtrs, dt := parseToolCalls(assistantResponse)
			displayText = dt
			// Convert pointer slice to value slice
			for _, tc := range jsonToolCallPtrs {
				toolCalls = append(toolCalls, *tc)
			}
		} else {
			displayText = cleanResponse(assistantResponse)
		}

		// If no tool calls, render and print response
		if len(toolCalls) == 0 {
			if displayText != "" {
				// Render with Glamour for syntax highlighting
				fmt.Println(ui.RenderMarkdown(displayText))
			}
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: assistantResponse,
			})

			// Save assistant response to session
			if a.storage != nil && a.session != nil {
				assistantMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   assistantResponse,
					Timestamp: time.Now(),
				}
				a.storage.AddMessage(a.session.ID, assistantMsg)
			}
			break
		}

		// Display any text before tool calls
		if displayText != "" {
			fmt.Println(ui.RenderMarkdown(displayText))
		}

		// Build assistant message with tool calls for conversation
		assistantMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: assistantResponse,
		}
		if hasNativeToolCalls {
			assistantMsg.ToolCalls = message.ToolCalls
		}
		a.conversation = append(a.conversation, assistantMsg)

		// Save assistant message with tool calls to session (once, before execution)
		if a.storage != nil && a.session != nil {
			var toolCallRecords []storage.ToolCallRecord
			for _, tc := range toolCalls {
				toolCallRecords = append(toolCallRecords, storage.ToolCallRecord{
					ID:     tc.ID,
					Tool:   tc.Tool,
					Params: tc.Params,
				})
			}
			storageAssistantMsg := storage.ConversationMessage{
				Role:      "assistant",
				Content:   assistantResponse,
				Timestamp: time.Now(),
				ToolCalls: toolCallRecords,
			}
			a.storage.AddMessage(a.session.ID, storageAssistantMsg)
		}

		// Execute all tool calls
		totalTools := len(toolCalls)
		var toolMessages []openai.ChatCompletionMessage

		// Count tools requiring security audit for batch handling
		var auditBatch *ui.BatchAuditContext
		if a.mode == storage.ModeSecurity {
			auditCount := 0
			for _, tc := range toolCalls {
				cat := permissions.GetToolCategory(tc.Tool)
				if cat != permissions.CategoryRead {
					auditCount++
				}
			}
			if auditCount > 1 {
				auditBatch = &ui.BatchAuditContext{
					TotalTools:   auditCount,
					CurrentIndex: 0,
				}
			}
		}

		for idx, toolCall := range toolCalls {
			var result string
			var isError bool
			var duration int64

			// Check permission before executing
			if allowed, permResult := a.checkToolPermission(toolCall.Tool, toolCall.Params); !allowed {
				result = permResult
				isError = true
				duration = 0
			} else {
				// Update batch index for non-read operations
				if auditBatch != nil {
					cat := permissions.GetToolCategory(toolCall.Tool)
					if cat != permissions.CategoryRead {
						auditBatch.CurrentIndex++
					}
				}

				// Security audit enforcement (security mode only)
				// This is separate from permissions - even "always allow" tools get audited in security mode
				if auditAllowed, auditResult := a.checkSecurityAudit(toolCall.Tool, toolCall.Params, auditBatch); !auditAllowed {
					result = auditResult
					isError = false // User denial is not an error, it's a choice

					// Build tool result message for blocked operation
					if hasNativeToolCalls && toolCall.ID != "" {
						toolMessages = append(toolMessages, openai.ChatCompletionMessage{
							Role:       openai.ChatMessageRoleTool,
							Content:    result,
							ToolCallID: toolCall.ID,
						})
					}

					// Save blocked operation to session
					if a.storage != nil && a.session != nil {
						toolResultMsg := storage.ConversationMessage{
							Role:       "tool",
							Content:    result,
							Timestamp:  time.Now(),
							ToolCallID: toolCall.ID,
							ToolCall: &storage.ToolCallRecord{
								ID:       toolCall.ID,
								Tool:     toolCall.Tool,
								Params:   toolCall.Params,
								Result:   result,
								Duration: 0,
								Success:  false,
							},
						}
						a.storage.AddMessage(a.session.ID, toolResultMsg)
					}
					continue // Skip to next tool
				}

				// Handle edit preview for edit_file operations
				if toolCall.Tool == "edit_file" {
					proceed, previewResult, _ := a.handleEditPreview(toolCall.Params)
					if !proceed {
						result = previewResult
						isError = false // Cancellation is not an error
						duration = 0
						// Skip to next tool - DisplayEditCancelled already printed the message
						continue
					}
				}

				// Start tool execution spinner with progress
				var toolSpinner *ui.Spinner
				if a.enableSpinner {
					toolSpinner = ui.NewSpinner()
					if totalTools > 1 {
						toolSpinner.Start(fmt.Sprintf("Running %s (%d/%d)...", toolCall.Tool, idx+1, totalTools))
					} else {
						toolSpinner.Start(fmt.Sprintf("Running %s...", toolCall.Tool))
					}
				}

				// Inject security defaults for security tools
				execParams := toolCall.Params
				if tools.IsSecurityTool(toolCall.Tool) {
					defaultSeverity := viper.GetString("security.default_severity")
					execParams = tools.InjectSecurityDefaults(toolCall.Tool, toolCall.Params, defaultSeverity)
				}

				// Execute the tool
				startTime := time.Now()
				var err error
				result, err = a.toolRegistry.ExecuteTool(toolCall.Tool, execParams, a.workingDir)
				duration = time.Since(startTime).Milliseconds()
				isError = err != nil
				if isError {
					result = fmt.Sprintf("Error: %v", err)
				}

				// Stop tool spinner
				if toolSpinner != nil {
					toolSpinner.Stop()
				}
			}

			// Print concise tool status using renderer
			fmt.Println(a.renderer.FormatToolStatus(toolCall.Tool, toolCall.Params, result, isError))

			// Build tool result message
			if hasNativeToolCalls && toolCall.ID != "" {
				// Native function calling - use tool message format
				toolMessages = append(toolMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: toolCall.ID,
				})

				// Save tool response to session
				if a.storage != nil && a.session != nil {
					toolResultMsg := storage.ConversationMessage{
						Role:       "tool",
						Content:    result,
						Timestamp:  time.Now(),
						ToolCallID: toolCall.ID,
						ToolCall: &storage.ToolCallRecord{
							ID:       toolCall.ID,
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolResultMsg)
				}
			} else {
				// Fallback - aggregate results as user message
				if len(toolMessages) == 0 {
					toolMessages = append(toolMessages, openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleUser,
						Content: "",
					})
				}
				if totalTools > 1 {
					toolMessages[0].Content += fmt.Sprintf("[%d] %s result:\n%s\n\n", idx+1, toolCall.Tool, result)
				} else {
					toolMessages[0].Content = fmt.Sprintf("Tool result:\n%s", result)
				}

				// Save tool call result for fallback mode (legacy behavior)
				if a.storage != nil && a.session != nil {
					toolMsg := storage.ConversationMessage{
						Role:      "assistant",
						Content:   assistantResponse,
						Timestamp: time.Now(),
						ToolCall: &storage.ToolCallRecord{
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolMsg)
				}
			}
		}

		// Add tool result messages to conversation
		a.conversation = append(a.conversation, toolMessages...)
	}

	return nil
}

// checkToolPermission checks if a tool is allowed to execute and handles user prompts
// Returns (allowed, resultMessage) - if not allowed, resultMessage contains the denial message
func (a *Assistant) checkToolPermission(toolName string, params map[string]interface{}) (bool, string) {
	// If no permission manager, allow all (graceful degradation)
	if a.permMgr == nil {
		return true, ""
	}

	perm := a.permMgr.CheckPermission(toolName)

	switch perm {
	case permissions.PermissionAllow:
		return true, ""
	case permissions.PermissionDeny:
		ui.DisplayPermissionDenied(toolName)
		return false, fmt.Sprintf("Tool '%s' blocked by permission settings", toolName)
	case permissions.PermissionAsk:
		// Prompt user for permission
		choice := ui.PromptToolPermission(toolName, params)

		// Save permission if requested
		if choice.SavePerm != "" {
			var err error
			if choice.SaveScope == "tool" {
				err = a.permMgr.SetToolPermission(toolName, choice.SavePerm)
			} else if choice.SaveScope == "category" {
				err = a.permMgr.SetCategoryPermission(choice.Category, choice.SavePerm)
			}
			if err == nil {
				ui.DisplayPermissionSaved(choice.SaveScope, string(choice.Category), choice.SavePerm)
			}
		}

		if !choice.Allowed {
			return false, fmt.Sprintf("Tool '%s' denied by user", toolName)
		}
		return true, ""
	}

	return true, ""
}

// GetPermissionManager returns the permission manager for external access (e.g., REPL commands)
func (a *Assistant) GetPermissionManager() *permissions.Manager {
	return a.permMgr
}

// checkSecurityAudit enforces audit-first behavior in security mode
// This is called AFTER permission check passes, providing an additional layer of protection
// Returns (allowed, resultMessage) - if not allowed, resultMessage contains the denial message
func (a *Assistant) checkSecurityAudit(toolName string, params map[string]interface{}, batch *ui.BatchAuditContext) (bool, string) {
	// Only enforce in security mode
	if a.mode != storage.ModeSecurity {
		return true, ""
	}

	// Get the tool's category
	category := permissions.GetToolCategory(toolName)

	// Only audit non-read operations
	// Read operations are safe and don't require audit confirmation
	if category == permissions.CategoryRead {
		return true, ""
	}

	// Helper to record audit entry
	recordAudit := func(action storage.AuditAction) {
		if a.storage == nil || a.session == nil {
			return
		}
		entry := storage.AuditEntry{
			Timestamp:   time.Now(),
			ToolName:    toolName,
			Category:    string(category),
			Action:      action,
			Params:      params,
			Target:      extractAuditTarget(toolName, params),
			Implication: ui.GetSecurityImplication(toolName),
		}
		if batch != nil {
			entry.BatchIndex = batch.CurrentIndex
			entry.BatchTotal = batch.TotalTools
		}
		// Record asynchronously to avoid blocking
		go func() {
			_ = a.storage.AddAuditEntry(a.session.ID, entry)
		}()
	}

	// Check if batch decision already made
	if batch != nil {
		if batch.AllowRemaining {
			recordAudit(storage.AuditActionAllowAll)
			return true, ""
		}
		if batch.DenyRemaining {
			recordAudit(storage.AuditActionDenyAll)
			ui.DisplaySecurityAuditDenied(toolName)
			return false, fmt.Sprintf("Operation '%s' blocked by security audit (batch deny)", toolName)
		}
	}

	// Build security audit info
	auditInfo := &ui.SecurityAuditInfo{
		ToolName:    toolName,
		Category:    category,
		Params:      params,
		Implication: ui.GetSecurityImplication(toolName),
	}

	// Loop to handle "details" option
	for {
		choice := ui.PromptSecurityAuditBatch(auditInfo, batch)

		switch choice {
		case ui.SecurityAuditAllow:
			recordAudit(storage.AuditActionAllow)
			return true, ""
		case ui.SecurityAuditDeny:
			recordAudit(storage.AuditActionDeny)
			ui.DisplaySecurityAuditDenied(toolName)
			return false, fmt.Sprintf("Operation '%s' blocked by security audit", toolName)
		case ui.SecurityAuditAllowAll:
			recordAudit(storage.AuditActionAllowAll)
			if batch != nil {
				batch.AllowRemaining = true
				remaining := batch.TotalTools - batch.CurrentIndex
				if remaining > 0 {
					ui.DisplaySecurityAuditBatchAllowed(remaining)
				}
			}
			return true, ""
		case ui.SecurityAuditDenyAll:
			recordAudit(storage.AuditActionDenyAll)
			if batch != nil {
				batch.DenyRemaining = true
				remaining := batch.TotalTools - batch.CurrentIndex
				if remaining > 0 {
					ui.DisplaySecurityAuditBatchDenied(remaining)
				}
			}
			ui.DisplaySecurityAuditDenied(toolName)
			return false, fmt.Sprintf("Operation '%s' blocked by security audit (batch deny)", toolName)
		case ui.SecurityAuditDetails:
			ui.DisplaySecurityAuditDetails(auditInfo)
			// Loop continues to show prompt again
		}
	}
}

// extractAuditTarget extracts the primary target from tool parameters for audit logging
func extractAuditTarget(toolName string, params map[string]interface{}) string {
	// Map of tools to their primary target parameter
	targetParams := map[string][]string{
		// File operations
		"write_file":       {"file_path"},
		"append_file":      {"file_path"},
		"edit_file":        {"file_path"},
		"insert_lines":     {"file_path"},
		"replace_lines":    {"file_path"},
		"delete_lines":     {"file_path"},
		"copy_file":        {"source", "destination"},
		"move_file":        {"source", "destination"},
		"delete_file":      {"file_path"},
		"create_directory": {"path"},
		// Execution
		"execute_command": {"command"},
		// Git
		"git_add":    {"files"},
		"git_commit": {"message"},
		"git_branch": {"name", "action"},
		// Kubernetes
		"kubectl_apply":  {"resource", "file"},
		"kubectl_delete": {"resource", "name"},
		"kubectl_exec":   {"pod", "command"},
		// Docker
		"docker_build":   {"dockerfile", "tag"},
		"docker_compose": {"action"},
		"docker_exec":    {"container", "command"},
		// Terraform
		"terraform_apply":   {"target"},
		"terraform_destroy": {"target"},
		// Helm
		"helm_install": {"release", "chart"},
	}

	if paramNames, ok := targetParams[toolName]; ok {
		var targets []string
		for _, name := range paramNames {
			if val, ok := params[name]; ok {
				switch v := val.(type) {
				case string:
					if v != "" {
						targets = append(targets, v)
					}
				case []interface{}:
					for _, item := range v {
						if s, ok := item.(string); ok && s != "" {
							targets = append(targets, s)
						}
					}
				}
			}
		}
		if len(targets) > 0 {
			if len(targets) == 1 {
				return targets[0]
			}
			return fmt.Sprintf("%v", targets)
		}
	}

	return ""
}

// handleEditPreview handles the edit preview workflow for edit_file operations
// Returns (proceed, result, error) - if proceed is false, use result as the tool result
func (a *Assistant) handleEditPreview(params map[string]interface{}) (bool, string, error) {
	// Check if preview mode is enabled
	if !viper.GetBool("preview_edits") {
		return true, "", nil
	}

	// Extract parameters
	filePath, ok := params["file_path"].(string)
	if !ok {
		return true, "", nil // Let the tool handle the error
	}

	oldString, ok := params["old_string"].(string)
	if !ok || oldString == "" {
		return true, "", nil // Let the tool handle the error
	}

	newString, _ := params["new_string"].(string)

	// Resolve path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(a.workingDir, filePath)
	}

	// Read current file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return true, "", nil // Let the tool handle the error
	}

	oldContent := string(content)

	// Check if old_string exists
	if !strings.Contains(oldContent, oldString) {
		return true, "", nil // Let the tool handle the error
	}

	// Check preview threshold (number of lines changed)
	threshold := viper.GetInt("preview_threshold")
	oldLines := strings.Count(oldString, "\n") + 1
	newLines := strings.Count(newString, "\n") + 1
	linesChanged := oldLines
	if newLines > oldLines {
		linesChanged = newLines
	}

	// Skip preview if below threshold (unless threshold is 0 which means always preview)
	if threshold > 0 && linesChanged < threshold {
		return true, "", nil
	}

	// Generate new content for preview
	replaceAll := false
	if ra, ok := params["replace_all"].(bool); ok {
		replaceAll = ra
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, oldString, newString)
	} else {
		newContent = strings.Replace(oldContent, oldString, newString, 1)
	}

	// Create preview
	preview := &ui.EditPreview{
		FilePath:   filePath,
		OldContent: oldContent,
		NewContent: newContent,
		OldString:  oldString,
		NewString:  newString,
	}

	// Display preview and get user choice
	choice := ui.DisplayEditPreview(preview)

	switch choice {
	case ui.EditPreviewApply:
		// User approved - proceed with edit
		return true, "", nil

	case ui.EditPreviewCancel:
		// User cancelled
		ui.DisplayEditCancelled(filePath)
		return false, fmt.Sprintf("Edit cancelled by user: %s", filePath), nil

	case ui.EditPreviewBackupThenApply:
		// Create backup first, then proceed
		if a.storage != nil {
			backupPath, err := a.storage.CreateBackup(absPath)
			if err != nil {
				return false, fmt.Sprintf("Failed to create backup: %v", err), err
			}
			ui.DisplayBackupCreated(backupPath)
		}
		return true, "", nil
	}

	return true, "", nil
}

// SendMessageForPlanning sends a prompt to the LLM for planning without tool execution.
// This is used by TaskPlanner to generate execution plans from natural language.
// The response is returned as a string without modifying the main conversation.
func (a *Assistant) SendMessageForPlanning(prompt string) (string, error) {
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	// Build a minimal conversation with just the planning prompt
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "You are a task planning assistant. Generate structured JSON plans for executing multi-step tasks. Be concise and practical.",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: prompt,
		},
	}

	// Create request without tools - pure text generation for planning
	req := openai.ChatCompletionRequest{
		Model:    a.model,
		Messages: messages,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	// Use streaming to accumulate the response
	stream, err := a.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create planning stream: %w", err)
	}
	defer stream.Close()

	var response strings.Builder

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("planning stream error: %w", err)
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				response.WriteString(delta.Content)
			}
		}

		// Track usage
		if chunk.Usage != nil {
			a.sessionUsage.PromptTokens += chunk.Usage.PromptTokens
			a.sessionUsage.CompletionTokens += chunk.Usage.CompletionTokens
			a.sessionUsage.TotalTokens += chunk.Usage.TotalTokens
		}
	}

	return response.String(), nil
}

// AnalyzeImages sends images to the LLM for analysis without affecting the conversation
// This is used for screen monitoring where we need standalone analysis
func (a *Assistant) AnalyzeImages(prompt string, images []*ImageData) (string, error) {
	if len(images) == 0 {
		return "", fmt.Errorf("no images provided")
	}

	// Build message with images
	parts := make([]openai.ChatMessagePart, 0, len(images)+1)

	// Add text prompt
	parts = append(parts, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: prompt,
	})

	// Add images
	for _, img := range images {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL:    img.ToDataURL(),
				Detail: openai.ImageURLDetailAuto,
			},
		})
	}

	// Create standalone message (not part of main conversation)
	messages := []openai.ChatCompletionMessage{
		{
			Role:         openai.ChatMessageRoleUser,
			MultiContent: parts,
		},
	}

	// Create context with timeout
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 2*time.Minute)
	defer cancel()

	// Make request without tools (simple analysis)
	req := openai.ChatCompletionRequest{
		Model:    a.model,
		Messages: messages,
	}

	resp, err := a.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("analysis request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}

	// Track usage - use API response if available, otherwise estimate
	if resp.Usage.TotalTokens > 0 {
		a.sessionUsage.PromptTokens += resp.Usage.PromptTokens
		a.sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
		a.sessionUsage.TotalTokens += resp.Usage.TotalTokens
	} else {
		// Estimate tokens when API doesn't return usage (common with Ollama vision)
		// Rough estimation: ~4 chars per token for text, ~1000 tokens per image
		promptTokens := len(prompt)/4 + len(images)*1000
		completionTokens := len(resp.Choices[0].Message.Content) / 4
		totalTokens := promptTokens + completionTokens

		a.sessionUsage.PromptTokens += promptTokens
		a.sessionUsage.CompletionTokens += completionTokens
		a.sessionUsage.TotalTokens += totalTokens
	}

	return resp.Choices[0].Message.Content, nil
}
