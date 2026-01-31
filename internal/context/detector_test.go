package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProject_Go(t *testing.T) {
	// Create temp directory with go.mod
	tmpDir := t.TempDir()
	goMod := `module github.com/example/myproject

go 1.21

require (
	github.com/spf13/cobra v1.7.0
	github.com/spf13/viper v1.16.0
)
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Go" {
		t.Errorf("Expected type 'Go', got '%s'", info.Type)
	}
	if info.ModuleName != "github.com/example/myproject" {
		t.Errorf("Expected module name 'github.com/example/myproject', got '%s'", info.ModuleName)
	}
	if len(info.DetectedTools) == 0 {
		t.Error("Expected detected tools, got none")
	}
	// Should include core file tools
	if !contains(info.DetectedTools, "read_file") {
		t.Error("Expected read_file in detected tools")
	}
}

func TestDetectProject_NodeJS(t *testing.T) {
	tmpDir := t.TempDir()
	packageJSON := `{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "^4.17.21"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Node.js" {
		t.Errorf("Expected type 'Node.js', got '%s'", info.Type)
	}
	if info.ModuleName != "my-app" {
		t.Errorf("Expected module name 'my-app', got '%s'", info.ModuleName)
	}
}

func TestDetectProject_TypeScript(t *testing.T) {
	tmpDir := t.TempDir()
	packageJSON := `{
  "name": "ts-app",
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "TypeScript" {
		t.Errorf("Expected type 'TypeScript', got '%s'", info.Type)
	}
}

func TestDetectProject_Python(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "requirements.txt"), []byte("flask\ndjango\n"), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Python" {
		t.Errorf("Expected type 'Python', got '%s'", info.Type)
	}
}

func TestDetectProject_Rust(t *testing.T) {
	tmpDir := t.TempDir()
	cargoToml := `[package]
name = "my-crate"
version = "0.1.0"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Cargo.toml"), []byte(cargoToml), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Rust" {
		t.Errorf("Expected type 'Rust', got '%s'", info.Type)
	}
	if info.ModuleName != "my-crate" {
		t.Errorf("Expected module name 'my-crate', got '%s'", info.ModuleName)
	}
}

func TestDetectProject_Java(t *testing.T) {
	tmpDir := t.TempDir()
	pomXML := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
</project>
`
	if err := os.WriteFile(filepath.Join(tmpDir, "pom.xml"), []byte(pomXML), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Java" {
		t.Errorf("Expected type 'Java', got '%s'", info.Type)
	}
}

func TestDetectProject_Terraform(t *testing.T) {
	tmpDir := t.TempDir()
	mainTF := `provider "aws" {
  region = "us-west-2"
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Terraform" {
		t.Errorf("Expected type 'Terraform', got '%s'", info.Type)
	}
	// Should detect terraform tools
	hasTerraformTool := false
	for _, tool := range info.DetectedTools {
		if tool == "terraform_plan" || tool == "terraform_apply" {
			hasTerraformTool = true
			break
		}
	}
	if !hasTerraformTool {
		t.Error("Expected terraform tools in detected tools")
	}
}

func TestDetectFrameworks_Docker(t *testing.T) {
	tmpDir := t.TempDir()
	// Go project with Docker
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM golang:1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Go" {
		t.Errorf("Expected type 'Go', got '%s'", info.Type)
	}
	if !contains(info.Frameworks, "docker") {
		t.Error("Expected 'docker' in frameworks")
	}
	// Should include docker tools
	if !contains(info.DetectedTools, "docker_build") {
		t.Error("Expected docker_build in detected tools")
	}
}

func TestDetectFrameworks_Kubernetes(t *testing.T) {
	tmpDir := t.TempDir()
	// Create k8s directory
	k8sDir := filepath.Join(tmpDir, "k8s")
	if err := os.MkdirAll(k8sDir, 0755); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if !contains(info.Frameworks, "kubernetes") {
		t.Error("Expected 'kubernetes' in frameworks")
	}
	// Should include kubectl tools
	if !contains(info.DetectedTools, "kubectl_get") {
		t.Error("Expected kubectl_get in detected tools")
	}
}

func TestDetectFrameworks_Helm(t *testing.T) {
	tmpDir := t.TempDir()
	chartYaml := `apiVersion: v2
name: my-chart
version: 0.1.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Chart.yaml"), []byte(chartYaml), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if !contains(info.Frameworks, "helm") {
		t.Error("Expected 'helm' in frameworks")
	}
	if !contains(info.DetectedTools, "helm_list") {
		t.Error("Expected helm_list in detected tools")
	}
}

func TestDetectProject_Unknown(t *testing.T) {
	tmpDir := t.TempDir()
	// Empty directory

	info := DetectProject(tmpDir)

	if info.Type != "Unknown" {
		t.Errorf("Expected type 'Unknown', got '%s'", info.Type)
	}
	// Should still have core file tools
	if !contains(info.DetectedTools, "read_file") {
		t.Error("Expected read_file in detected tools even for unknown projects")
	}
}

func TestDetectProject_MultiFramework(t *testing.T) {
	tmpDir := t.TempDir()
	// Go project with Docker, Kubernetes, and Terraform
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM golang:1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "k8s"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(`provider "aws" {}`), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectProject(tmpDir)

	if info.Type != "Go" {
		t.Errorf("Expected type 'Go', got '%s'", info.Type)
	}
	if !contains(info.Frameworks, "docker") {
		t.Error("Expected 'docker' in frameworks")
	}
	if !contains(info.Frameworks, "kubernetes") {
		t.Error("Expected 'kubernetes' in frameworks")
	}
	if !contains(info.Frameworks, "terraform") {
		t.Error("Expected 'terraform' in frameworks")
	}

	// Should have tools from all frameworks
	expectedTools := []string{"docker_build", "kubectl_get", "terraform_plan"}
	for _, expected := range expectedTools {
		if !contains(info.DetectedTools, expected) {
			t.Errorf("Expected %s in detected tools", expected)
		}
	}
}
