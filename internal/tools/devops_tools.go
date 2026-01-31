package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// =============================================================================
// KUBERNETES TOOLS
// =============================================================================

// KubectlGet retrieves Kubernetes resources
func KubectlGet(params map[string]interface{}, workingDir string) (string, error) {
	resource, ok := params["resource"].(string)
	if !ok || resource == "" {
		return "", fmt.Errorf("resource parameter is required (e.g., pods, deployments, services)")
	}

	args := []string{"get", resource}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	// Optional name filter
	if name, ok := params["name"].(string); ok && name != "" {
		args = append(args, name)
	}

	// Output format (default: wide for more info)
	output := "wide"
	if o, ok := params["output"].(string); ok && o != "" {
		output = o
	}
	args = append(args, "-o", output)

	// Optional label selector
	if selector, ok := params["selector"].(string); ok && selector != "" {
		args = append(args, "-l", selector)
	}

	// All namespaces flag
	if allNs, ok := params["all_namespaces"].(bool); ok && allNs {
		args = append(args, "--all-namespaces")
	}

	return runCommand("kubectl", args, workingDir, 30*time.Second)
}

// KubectlApply applies Kubernetes manifests
func KubectlApply(params map[string]interface{}, workingDir string) (string, error) {
	file, ok := params["file"].(string)
	if !ok || file == "" {
		return "", fmt.Errorf("file parameter is required")
	}

	args := []string{"apply", "-f", file}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	// Dry run option
	if dryRun, ok := params["dry_run"].(bool); ok && dryRun {
		args = append(args, "--dry-run=client")
	}

	return runCommand("kubectl", args, workingDir, 60*time.Second)
}

// KubectlDelete deletes Kubernetes resources
func KubectlDelete(params map[string]interface{}, workingDir string) (string, error) {
	resource, ok := params["resource"].(string)
	if !ok || resource == "" {
		return "", fmt.Errorf("resource parameter is required")
	}

	args := []string{"delete", resource}

	// Resource name
	if name, ok := params["name"].(string); ok && name != "" {
		args = append(args, name)
	}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	// Force deletion
	if force, ok := params["force"].(bool); ok && force {
		args = append(args, "--force", "--grace-period=0")
	}

	return runCommand("kubectl", args, workingDir, 60*time.Second)
}

// KubectlDescribe describes Kubernetes resources
func KubectlDescribe(params map[string]interface{}, workingDir string) (string, error) {
	resource, ok := params["resource"].(string)
	if !ok || resource == "" {
		return "", fmt.Errorf("resource parameter is required")
	}

	args := []string{"describe", resource}

	// Resource name
	if name, ok := params["name"].(string); ok && name != "" {
		args = append(args, name)
	}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	return runCommand("kubectl", args, workingDir, 30*time.Second)
}

// KubectlLogs retrieves pod logs
func KubectlLogs(params map[string]interface{}, workingDir string) (string, error) {
	pod, ok := params["pod"].(string)
	if !ok || pod == "" {
		return "", fmt.Errorf("pod parameter is required")
	}

	args := []string{"logs", pod}

	// Optional container
	if container, ok := params["container"].(string); ok && container != "" {
		args = append(args, "-c", container)
	}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	// Tail lines
	if tail, ok := params["tail"].(float64); ok {
		args = append(args, "--tail", fmt.Sprintf("%d", int(tail)))
	} else if tail, ok := params["tail"].(int); ok {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	// Previous container logs
	if previous, ok := params["previous"].(bool); ok && previous {
		args = append(args, "--previous")
	}

	return runCommand("kubectl", args, workingDir, 30*time.Second)
}

// KubectlExec executes a command in a pod
func KubectlExec(params map[string]interface{}, workingDir string) (string, error) {
	pod, ok := params["pod"].(string)
	if !ok || pod == "" {
		return "", fmt.Errorf("pod parameter is required")
	}

	command, ok := params["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command parameter is required")
	}

	args := []string{"exec", pod}

	// Optional container
	if container, ok := params["container"].(string); ok && container != "" {
		args = append(args, "-c", container)
	}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	args = append(args, "--", "sh", "-c", command)

	return runCommand("kubectl", args, workingDir, 60*time.Second)
}

// HelmList lists Helm releases
func HelmList(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"list"}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace)
	}

	// All namespaces
	if allNs, ok := params["all_namespaces"].(bool); ok && allNs {
		args = append(args, "--all-namespaces")
	}

	return runCommand("helm", args, workingDir, 30*time.Second)
}

// HelmInstall installs a Helm chart
func HelmInstall(params map[string]interface{}, workingDir string) (string, error) {
	release, ok := params["release"].(string)
	if !ok || release == "" {
		return "", fmt.Errorf("release parameter is required")
	}

	chart, ok := params["chart"].(string)
	if !ok || chart == "" {
		return "", fmt.Errorf("chart parameter is required")
	}

	args := []string{"install", release, chart}

	// Optional namespace
	if namespace, ok := params["namespace"].(string); ok && namespace != "" {
		args = append(args, "-n", namespace, "--create-namespace")
	}

	// Values file
	if values, ok := params["values"].(string); ok && values != "" {
		args = append(args, "-f", values)
	}

	// Set values
	if set, ok := params["set"].(string); ok && set != "" {
		args = append(args, "--set", set)
	}

	// Dry run
	if dryRun, ok := params["dry_run"].(bool); ok && dryRun {
		args = append(args, "--dry-run")
	}

	return runCommand("helm", args, workingDir, 120*time.Second)
}

// =============================================================================
// TERRAFORM TOOLS
// =============================================================================

// TerraformInit initializes a Terraform working directory
func TerraformInit(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"init", "-no-color"}

	// Backend config
	if backendConfig, ok := params["backend_config"].(string); ok && backendConfig != "" {
		args = append(args, "-backend-config="+backendConfig)
	}

	// Upgrade providers
	if upgrade, ok := params["upgrade"].(bool); ok && upgrade {
		args = append(args, "-upgrade")
	}

	return runCommand("terraform", args, workingDir, 120*time.Second)
}

// TerraformPlan generates a Terraform execution plan
func TerraformPlan(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"plan", "-no-color"}

	// Target specific resources
	if target, ok := params["target"].(string); ok && target != "" {
		args = append(args, "-target="+target)
	}

	// Variable file
	if varFile, ok := params["var_file"].(string); ok && varFile != "" {
		args = append(args, "-var-file="+varFile)
	}

	// Output plan file
	if out, ok := params["out"].(string); ok && out != "" {
		args = append(args, "-out="+out)
	}

	// Destroy plan
	if destroy, ok := params["destroy"].(bool); ok && destroy {
		args = append(args, "-destroy")
	}

	return runCommand("terraform", args, workingDir, 300*time.Second)
}

// TerraformApply applies Terraform changes
func TerraformApply(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"apply", "-no-color"}

	// Plan file
	if planFile, ok := params["plan_file"].(string); ok && planFile != "" {
		args = append(args, planFile)
	}

	// Auto approve (use with caution)
	if autoApprove, ok := params["auto_approve"].(bool); ok && autoApprove {
		args = append(args, "-auto-approve")
	}

	// Target specific resources
	if target, ok := params["target"].(string); ok && target != "" {
		args = append(args, "-target="+target)
	}

	return runCommand("terraform", args, workingDir, 600*time.Second)
}

// TerraformDestroy destroys Terraform-managed infrastructure
func TerraformDestroy(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"destroy", "-no-color"}

	// Target specific resources
	if target, ok := params["target"].(string); ok && target != "" {
		args = append(args, "-target="+target)
	}

	// Auto approve (use with extreme caution)
	if autoApprove, ok := params["auto_approve"].(bool); ok && autoApprove {
		args = append(args, "-auto-approve")
	}

	return runCommand("terraform", args, workingDir, 600*time.Second)
}

// TerraformOutput displays Terraform outputs
func TerraformOutput(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"output", "-no-color"}

	// Specific output name
	if name, ok := params["name"].(string); ok && name != "" {
		args = append(args, name)
	}

	// JSON format
	if jsonOutput, ok := params["json"].(bool); ok && jsonOutput {
		args = append(args, "-json")
	}

	return runCommand("terraform", args, workingDir, 30*time.Second)
}

// TerraformState manages Terraform state
func TerraformState(params map[string]interface{}, workingDir string) (string, error) {
	subcommand, ok := params["subcommand"].(string)
	if !ok || subcommand == "" {
		subcommand = "list"
	}

	args := []string{"state", subcommand}

	// Resource address for show/rm commands
	if address, ok := params["address"].(string); ok && address != "" {
		args = append(args, address)
	}

	return runCommand("terraform", args, workingDir, 30*time.Second)
}

// =============================================================================
// DOCKER TOOLS
// =============================================================================

// DockerBuild builds a Docker image
func DockerBuild(params map[string]interface{}, workingDir string) (string, error) {
	tag, ok := params["tag"].(string)
	if !ok || tag == "" {
		return "", fmt.Errorf("tag parameter is required")
	}

	args := []string{"build", "-t", tag}

	// Dockerfile path
	if dockerfile, ok := params["dockerfile"].(string); ok && dockerfile != "" {
		args = append(args, "-f", dockerfile)
	}

	// Build context (default to current directory)
	buildContext := "."
	if ctx, ok := params["context"].(string); ok && ctx != "" {
		buildContext = ctx
	}

	// Build args
	if buildArg, ok := params["build_arg"].(string); ok && buildArg != "" {
		args = append(args, "--build-arg", buildArg)
	}

	// No cache
	if noCache, ok := params["no_cache"].(bool); ok && noCache {
		args = append(args, "--no-cache")
	}

	args = append(args, buildContext)

	return runCommand("docker", args, workingDir, 600*time.Second)
}

// DockerPs lists Docker containers
func DockerPs(params map[string]interface{}, workingDir string) (string, error) {
	args := []string{"ps"}

	// Show all containers (including stopped)
	if all, ok := params["all"].(bool); ok && all {
		args = append(args, "-a")
	}

	// Filter
	if filter, ok := params["filter"].(string); ok && filter != "" {
		args = append(args, "-f", filter)
	}

	// Format
	args = append(args, "--format", "table {{.ID}}\t{{.Image}}\t{{.Status}}\t{{.Names}}")

	return runCommand("docker", args, workingDir, 30*time.Second)
}

// DockerLogs retrieves container logs
func DockerLogs(params map[string]interface{}, workingDir string) (string, error) {
	container, ok := params["container"].(string)
	if !ok || container == "" {
		return "", fmt.Errorf("container parameter is required")
	}

	args := []string{"logs", container}

	// Tail lines
	if tail, ok := params["tail"].(float64); ok {
		args = append(args, "--tail", fmt.Sprintf("%d", int(tail)))
	} else if tail, ok := params["tail"].(int); ok {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	// Timestamps
	if timestamps, ok := params["timestamps"].(bool); ok && timestamps {
		args = append(args, "-t")
	}

	return runCommand("docker", args, workingDir, 30*time.Second)
}

// DockerCompose runs Docker Compose commands
func DockerCompose(params map[string]interface{}, workingDir string) (string, error) {
	subcommand, ok := params["subcommand"].(string)
	if !ok || subcommand == "" {
		return "", fmt.Errorf("subcommand parameter is required (up, down, ps, logs, etc.)")
	}

	args := []string{"compose"}

	// Compose file
	if file, ok := params["file"].(string); ok && file != "" {
		args = append(args, "-f", file)
	}

	args = append(args, subcommand)

	// Detach mode for up
	if subcommand == "up" {
		if detach, ok := params["detach"].(bool); ok && detach {
			args = append(args, "-d")
		}
	}

	// Service name
	if service, ok := params["service"].(string); ok && service != "" {
		args = append(args, service)
	}

	return runCommand("docker", args, workingDir, 120*time.Second)
}

// DockerExec executes a command in a running container
func DockerExec(params map[string]interface{}, workingDir string) (string, error) {
	container, ok := params["container"].(string)
	if !ok || container == "" {
		return "", fmt.Errorf("container parameter is required")
	}

	command, ok := params["command"].(string)
	if !ok || command == "" {
		command = "sh"
	}

	args := []string{"exec", container, "sh", "-c", command}

	return runCommand("docker", args, workingDir, 60*time.Second)
}

// =============================================================================
// CLOUD PROVIDER TOOLS
// =============================================================================

// AwsCli runs AWS CLI commands
func AwsCli(params map[string]interface{}, workingDir string) (string, error) {
	service, ok := params["service"].(string)
	if !ok || service == "" {
		return "", fmt.Errorf("service parameter is required (e.g., s3, ec2, iam)")
	}

	command, ok := params["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command parameter is required")
	}

	args := []string{service, command}

	// Additional arguments
	if argsStr, ok := params["args"].(string); ok && argsStr != "" {
		args = append(args, strings.Fields(argsStr)...)
	}

	// Region
	if region, ok := params["region"].(string); ok && region != "" {
		args = append(args, "--region", region)
	}

	// Profile
	if profile, ok := params["profile"].(string); ok && profile != "" {
		args = append(args, "--profile", profile)
	}

	return runCommand("aws", args, workingDir, 60*time.Second)
}

// AwsEcs runs AWS ECS commands
func AwsEcs(params map[string]interface{}, workingDir string) (string, error) {
	subcommand, ok := params["subcommand"].(string)
	if !ok || subcommand == "" {
		return "", fmt.Errorf("subcommand parameter is required (e.g., list-clusters, describe-services)")
	}

	args := []string{"ecs", subcommand}

	// Cluster
	if cluster, ok := params["cluster"].(string); ok && cluster != "" {
		args = append(args, "--cluster", cluster)
	}

	// Service
	if service, ok := params["service"].(string); ok && service != "" {
		args = append(args, "--services", service)
	}

	// Region
	if region, ok := params["region"].(string); ok && region != "" {
		args = append(args, "--region", region)
	}

	return runCommand("aws", args, workingDir, 60*time.Second)
}

// AwsEks runs AWS EKS commands
func AwsEks(params map[string]interface{}, workingDir string) (string, error) {
	subcommand, ok := params["subcommand"].(string)
	if !ok || subcommand == "" {
		return "", fmt.Errorf("subcommand parameter is required (e.g., describe-cluster, list-clusters)")
	}

	args := []string{"eks", subcommand}

	// Cluster name
	if name, ok := params["name"].(string); ok && name != "" {
		args = append(args, "--name", name)
	}

	// Region
	if region, ok := params["region"].(string); ok && region != "" {
		args = append(args, "--region", region)
	}

	return runCommand("aws", args, workingDir, 60*time.Second)
}

// AzCli runs Azure CLI commands
func AzCli(params map[string]interface{}, workingDir string) (string, error) {
	group, ok := params["group"].(string)
	if !ok || group == "" {
		return "", fmt.Errorf("group parameter is required (e.g., vm, storage, network)")
	}

	command, ok := params["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command parameter is required")
	}

	args := []string{group, command}

	// Additional arguments
	if argsStr, ok := params["args"].(string); ok && argsStr != "" {
		args = append(args, strings.Fields(argsStr)...)
	}

	// Resource group
	if resourceGroup, ok := params["resource_group"].(string); ok && resourceGroup != "" {
		args = append(args, "--resource-group", resourceGroup)
	}

	return runCommand("az", args, workingDir, 60*time.Second)
}

// AzAks runs Azure AKS commands
func AzAks(params map[string]interface{}, workingDir string) (string, error) {
	subcommand, ok := params["subcommand"].(string)
	if !ok || subcommand == "" {
		return "", fmt.Errorf("subcommand parameter is required (e.g., show, list, get-credentials)")
	}

	args := []string{"aks", subcommand}

	// Cluster name
	if name, ok := params["name"].(string); ok && name != "" {
		args = append(args, "--name", name)
	}

	// Resource group
	if resourceGroup, ok := params["resource_group"].(string); ok && resourceGroup != "" {
		args = append(args, "--resource-group", resourceGroup)
	}

	return runCommand("az", args, workingDir, 60*time.Second)
}

// GcloudCli runs Google Cloud CLI commands
func GcloudCli(params map[string]interface{}, workingDir string) (string, error) {
	component, ok := params["component"].(string)
	if !ok || component == "" {
		return "", fmt.Errorf("component parameter is required (e.g., compute, storage, container)")
	}

	command, ok := params["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command parameter is required")
	}

	args := []string{component}
	args = append(args, strings.Fields(command)...)

	// Project
	if project, ok := params["project"].(string); ok && project != "" {
		args = append(args, "--project", project)
	}

	// Zone
	if zone, ok := params["zone"].(string); ok && zone != "" {
		args = append(args, "--zone", zone)
	}

	// Region
	if region, ok := params["region"].(string); ok && region != "" {
		args = append(args, "--region", region)
	}

	return runCommand("gcloud", args, workingDir, 60*time.Second)
}

// GkeCli runs GKE-specific commands
func GkeCli(params map[string]interface{}, workingDir string) (string, error) {
	subcommand, ok := params["subcommand"].(string)
	if !ok || subcommand == "" {
		return "", fmt.Errorf("subcommand parameter is required (e.g., clusters list, clusters describe)")
	}

	args := []string{"container"}
	args = append(args, strings.Fields(subcommand)...)

	// Cluster name
	if cluster, ok := params["cluster"].(string); ok && cluster != "" {
		args = append(args, "--cluster", cluster)
	}

	// Zone
	if zone, ok := params["zone"].(string); ok && zone != "" {
		args = append(args, "--zone", zone)
	}

	// Project
	if project, ok := params["project"].(string); ok && project != "" {
		args = append(args, "--project", project)
	}

	return runCommand("gcloud", args, workingDir, 60*time.Second)
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// runCommand executes a command with timeout and returns the output
func runCommand(name string, args []string, workingDir string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Combine stdout and stderr for output
	var result strings.Builder
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString(stderr.String())
	}

	if ctx.Err() == context.DeadlineExceeded {
		return result.String(), fmt.Errorf("command timed out after %v", timeout)
	}

	if err != nil {
		if result.Len() > 0 {
			return "", fmt.Errorf("%s %s failed: %w\n%s", name, args[0], err, result.String())
		}
		return "", fmt.Errorf("%s %s failed: %w", name, args[0], err)
	}

	if result.Len() == 0 {
		return fmt.Sprintf("%s %s completed successfully", name, args[0]), nil
	}

	return result.String(), nil
}

// resolvePath resolves a path relative to the working directory
func resolvePath(path, workingDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workingDir, path)
}
