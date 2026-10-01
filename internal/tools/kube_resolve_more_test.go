package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestKubeconfigEnvOfTwoKubeconfigsOrARelativeOne(t *testing.T) {
	dir := t.TempDir()
	r := newKubeResolver(context.Background(), dir)
	if env, ok := r.kubeconfigEnv("*"); ok || env != nil {
		t.Fatalf("two different kubeconfigs: %v %v", env, ok)
	}
	writeTestFile(t, filepath.Join(dir, "kc.yaml"), "apiVersion: v1\n", 0o600)
	env, ok := r.kubeconfigEnv("kc.yaml")
	if !ok || len(env) != 1 || env[0] != "KUBECONFIG="+filepath.Join(dir, "kc.yaml") {
		t.Fatalf("a relative kubeconfig resolves against the working directory: %v %v", env, ok)
	}
}

// TestKubeconfigEnvLeavesAPathTheShellComputes: a kubeconfig path that still holds a $ or a
// backtick after the home expansion is computed when the command runs, so it is not resolved, even
// when a file of that literal name exists.
func TestKubeconfigEnvLeavesAPathTheShellComputes(t *testing.T) {
	dir := t.TempDir()
	r := newKubeResolver(context.Background(), dir)
	for _, path := range []string{"$KUBE_DIR/config", "`pwd`/config"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(dir, path), "apiVersion: v1\n", 0o600)
		if env, ok := r.kubeconfigEnv(path); ok || env != nil {
			t.Errorf("%s: %v %v", path, env, ok)
		}
	}
}

// TestCurrentContextThatKubectlLeavesEmpty: a kubeconfig without a current context leaves the
// context unresolved rather than resolved to "".
func TestCurrentContextThatKubectlLeavesEmpty(t *testing.T) {
	fakeBin(t, "kubectl", "#!/bin/sh\nexit 0\n")
	r := newKubeResolver(context.Background(), t.TempDir())
	if current, ok := r.currentContext("", nil); ok || current != "" {
		t.Fatalf("%q %v", current, ok)
	}
}

// TestNamespaceWhenKubectlFailsOrSaysNothing: a context that sets no namespace is in "default"; when
// kubectl cannot say, the namespace is every namespace, "*".
func TestNamespaceWhenKubectlFailsOrSaysNothing(t *testing.T) {
	fakeBin(t, "kubectl", "#!/bin/sh\n[ \"$KUBECTL_FAILS\" = 1 ] && exit 1\nexit 0\n")
	r := newKubeResolver(context.Background(), t.TempDir())
	if ns := r.namespace("", nil, ""); ns != "default" {
		t.Fatalf("no namespace set: %q", ns)
	}
	t.Setenv("KUBECTL_FAILS", "1")
	if ns := r.namespace("", nil, "prod"); ns != "*" {
		t.Fatalf("kubectl failed: %q", ns)
	}
	if ns := r.namespace("", nil, ""); ns != "default" {
		t.Fatalf("a resolved namespace is read once per resolver: %q", ns)
	}
}
