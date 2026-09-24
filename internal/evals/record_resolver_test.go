package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKubectlResolverSelectsByPhase pins ruling P3-R61 C2: a phase=<value> term becomes a
// --field-selector on status.phase, the label terms one -l flag and ns= the namespace, so
// {{pod app=cart ns=shop-v2 phase=Pending}} picks the pod that failed to start.
func TestKubectlResolverSelectsByPhase(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv")
	fakeKubectl(t, "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+argvFile+"'\necho cart-7d9f-abcde\n")
	name, err := KubectlResolver(context.Background(), "pod", "app=cart tier=web ns=shop-v2 phase=Pending")
	if err != nil || name != "cart-7d9f-abcde" {
		t.Fatalf("name %q err %v", name, err)
	}
	data, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"get", "pod", "-o", "jsonpath={.items[0].metadata.name}", "-n", "shop-v2", "-l", "app=cart,tier=web",
		"--field-selector", "status.phase=Pending"}
	if got := strings.Split(strings.TrimSpace(string(data)), "\n"); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv %q, want %q", got, want)
	}
}
