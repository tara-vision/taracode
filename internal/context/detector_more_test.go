package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// projectWith writes files (name to content) into a fresh directory.
func projectWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectPrimaryTypes(t *testing.T) {
	tests := []struct {
		name   string
		files  map[string]string
		typ    string
		module string
	}{
		{"node with dependencies", map[string]string{"package.json": `{"name":"shop","dependencies":{"lodash":"1","express":"4"}}`},
			"Node.js", "shop"},
		{"node without a name", map[string]string{"package.json": `not json`}, "Node.js", ""},
		{"pyproject", map[string]string{"pyproject.toml": "[project]\nname = \"svc\"\n"}, "Python", ""},
		{"requirements", map[string]string{"requirements.txt": "flask\n"}, "Python", ""},
		{"setup.py", map[string]string{"setup.py": "setup()\n"}, "Python", ""},
		{"cargo", map[string]string{"Cargo.toml": "[package]\nname = \"tool\"\nversion = \"0.1.0\"\n"}, "Rust", "tool"},
		{"cargo without a name", map[string]string{"Cargo.toml": "[workspace]\n"}, "Rust", ""},
		{"gradle", map[string]string{"build.gradle": "plugins {}\n"}, "Java", ""},
		{"gradle kotlin", map[string]string{"build.gradle.kts": "plugins {}\n"}, "Kotlin", ""},
		{"terraform in a subdirectory", map[string]string{"infra/main.tf": "resource \"x\" \"y\" {}\n"}, "Terraform", ""},
	}
	for _, tt := range tests {
		info := DetectProject(projectWith(t, tt.files))
		if info.Type != tt.typ || info.ModuleName != tt.module {
			t.Errorf("%s: type %q module %q", tt.name, info.Type, info.ModuleName)
		}
	}
	node := DetectProject(projectWith(t, tests[0].files))
	deps := append([]string(nil), node.Dependencies...)
	sort.Strings(deps)
	if strings.Join(deps, ",") != "express,lodash" {
		t.Fatalf("node dependencies %v", node.Dependencies)
	}
}

func TestDetectFrameworksFindsEachKind(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		framework string
	}{
		{"compose yml", map[string]string{"docker-compose.yml": "services: {}\n"}, "docker"},
		{"compose yaml", map[string]string{"docker-compose.yaml": "services: {}\n"}, "docker"},
		{"k8s directory", map[string]string{"k8s/app.yaml": "kind: Deployment\n"}, "kubernetes"},
		{"service manifest", map[string]string{"web-service.yaml": "kind: Service\n"}, "kubernetes"},
		{"kustomization", map[string]string{"kustomization.yaml": "resources: []\n"}, "kubernetes"},
		{"charts directory", map[string]string{"charts/web/Chart.yaml": "name: web\n"}, "helm"},
		{"terraform beside go", map[string]string{"go.mod": "module m\n", "main.tf": "terraform {}\n"}, "terraform"},
		{"cdk", map[string]string{"cdk.json": "{}"}, "aws"},
		{"aws provider", map[string]string{"main.tf": "provider \"aws\" {}\n"}, "aws"},
		{"aws resource", map[string]string{"s3.tf": "resource \"aws_s3_bucket\" \"b\" {}\n"}, "aws"},
		{"azure pipelines", map[string]string{"azure-pipelines.yml": "trigger: [main]\n"}, "azure"},
		{"cloud build", map[string]string{"cloudbuild.yaml": "steps: []\n"}, "gcp"},
	}
	for _, tt := range tests {
		info := DetectProject(projectWith(t, tt.files))
		if !contains(info.Frameworks, tt.framework) {
			t.Errorf("%s: frameworks %v lack %s", tt.name, info.Frameworks, tt.framework)
		}
		for _, tool := range ToolMapping[tt.framework] {
			if !contains(info.DetectedTools, tool) {
				t.Errorf("%s: tools %v lack %s for %s", tt.name, info.DetectedTools, tool, tt.framework)
			}
		}
	}
	plain := DetectProject(projectWith(t, map[string]string{"notes.txt": "x"}))
	if len(plain.Frameworks) != 0 {
		t.Fatalf("no indicator, no framework: %v", plain.Frameworks)
	}
	if !contains(DetectProject(projectWith(t, map[string]string{"main.tf": "terraform {}\n"})).DetectedTools, "terraform") {
		t.Fatal("a terraform project maps to the terraform tool")
	}
}

func TestDetectionToleratesUnreadablePlaces(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if hasTerraformFiles(missing) || hasKubernetesFiles(missing) || hasTerraformAWSProvider(missing) {
		t.Fatal("a directory that cannot be read has nothing")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads whatever the mode says")
	}
	dir := projectWith(t, map[string]string{"locked/main.tf": "terraform {}\n", "secret.tf": "provider \"aws\" {}\n"})
	for _, path := range []string{filepath.Join(dir, "locked"), filepath.Join(dir, "secret.tf")} {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		p := path
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	}
	if hasTerraformAWSProvider(dir) {
		t.Fatal("an unreadable .tf file is skipped")
	}
	if !hasTerraformFiles(dir) {
		t.Fatal("secret.tf is a .tf file even when it cannot be read")
	}
	nested := projectWith(t, map[string]string{"locked/main.tf": "terraform {}\n"})
	if err := os.Chmod(filepath.Join(nested, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(nested, "locked"), 0o755) })
	if hasTerraformFiles(nested) {
		t.Fatal("a subdirectory that cannot be read is skipped")
	}
}
