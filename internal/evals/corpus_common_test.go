package evals

import (
	"path/filepath"
	"strings"
	"testing"
)

// committedCommonStore loads the corpus's own common fixture store, the one every task's replay falls back to.
func committedCommonStore(t *testing.T) *Store {
	t.Helper()
	dir := CommonDir(filepath.Clean(corpusRoot))
	if dir == "" {
		t.Fatal("the corpus has no common fixture store (evals/common/fixtures/index.yaml)")
	}
	s, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() == 0 {
		t.Fatal("the common fixture store is empty")
	}
	return s
}

// presenceProbes are the shell spellings that ask whether a tool is installed.
var presenceProbes = []string{"shell which ", "shell command -v ", "shell type "}

// clusterWords mark an answer that came from a Kubernetes API server, or from failing to reach one.
var clusterWords = []string{"Server Version:", `"serverVersion"`, "connection to the server", "Unable to connect to the server"}

// missingTool says why a common fixture answers for a tool the sandbox lacks, or "" when it does not: the shell's
// 127, or a presence probe, alone or opening a chain, that printed nothing before its exit status. A chain whose
// probe found the tool and whose later part failed is the tool's own answer.
func missingTool(sig, out string) string {
	if strings.Contains(out, "[exit status 127]") {
		return "the sandbox lacks the command (exit status 127)"
	}
	for _, p := range presenceProbes {
		if strings.HasPrefix(sig, p) && strings.HasPrefix(strings.TrimSpace(out), "[exit status ") {
			return "the presence probe finds nothing on the sandbox"
		}
	}
	return ""
}

// clusterAnswer says why a common fixture depends on a cluster, or "" when it does not.
func clusterAnswer(out string) string {
	for _, w := range clusterWords {
		if strings.Contains(out, w) {
			return "the answer depends on a cluster (" + w + ")"
		}
	}
	return ""
}

// TestTheCommonStoreRulesReadAnAnswer pins what the two rules below take for a missing tool and for a cluster's
// answer, on the spellings the sandbox answers with.
func TestTheCommonStoreRulesReadAnAnswer(t *testing.T) {
	cases := []struct {
		name, sig, out        string
		missing, fromACluster bool
	}{
		{"a command the shell cannot find", "aws --version", "sh: 1: aws: not found\n[exit status 127]", true, false},
		{"which finds nothing", "shell which aws", "[exit status 1]", true, false},
		{"a chain whose probe finds nothing", "shell which aws && aws --version", "[exit status 1]", true, false},
		{"a present tool whose later part fails", "shell which helm && helm --version",
			"/usr/local/bin/helm\nError: unknown flag: --version\n[exit status 1]", false, false},
		{"a present tool's flag error", "kubectl --version",
			"error: unknown flag: --version\nSee 'kubectl --help' for usage.\n[exit status 1]", false, false},
		{"which finds the tool", "shell which terraform", "/usr/local/bin/terraform", false, false},
		{"kubectl's client alone", "kubectl version --client", "Client Version: v1.37.1\nKustomize Version: v5.8.1",
			false, false},
		{"a cluster's server version", "kubectl version",
			"Client Version: v1.37.1\nKustomize Version: v5.8.1\nServer Version: v1.37.0", false, true},
		{"a server version in JSON", "kubectl version -o json",
			`{"clientVersion": {"gitVersion": "v1.37.1"}, "serverVersion": {"gitVersion": "v1.37.0"}}`, false, true},
		{"a cluster that does not answer", "kubectl version", "Client Version: v1.37.1\nThe connection to the server " +
			"localhost:8080 was refused - did you specify the right host or port?\n[exit status 1]", false, true},
		{"docker's own daemon", "shell docker version", "Client:\n Version:           29.1.3\n\nServer:\n Engine:\n" +
			"  Version:          29.1.3", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := missingTool(c.sig, c.out); (got != "") != c.missing {
				t.Errorf("missingTool(%q) = %q, want a problem: %v", c.sig, got, c.missing)
			}
			if got := clusterAnswer(c.out); (got != "") != c.fromACluster {
				t.Errorf("clusterAnswer(%q) = %q, want a problem: %v", c.sig, got, c.fromACluster)
			}
		})
	}
}

// TestTheCommonStoreRecordsOnlyToolsTheSandboxHas holds spec 1b: the common store answers every task, so it holds
// the tools the sandbox has; a tool the sandbox lacks is not recorded and a probe for it stays a miss. Recorded, its
// "not found" would tell a task whose world has the tool (aws in aws-iam-wildcard-policy) that it is missing.
func TestTheCommonStoreRecordsOnlyToolsTheSandboxHas(t *testing.T) {
	s := committedCommonStore(t)
	for _, f := range s.Fixtures() {
		out, _, ok := s.Lookup(f.Signature)
		if !ok {
			t.Errorf("%s: indexed but not readable", f.Signature)
			continue
		}
		if why := missingTool(f.Signature, out); why != "" {
			t.Errorf("%s: %s; drop the tool's calls from evals/common/calls.yaml", f.Signature, why)
		}
	}
}

// TestTheCommonStoreHoldsNoClusterAnswer: a cluster belongs to a task's world (container-crashed has none, spec 1c),
// and the common store answers every task, inside chains too, so for kubectl it records the client only.
func TestTheCommonStoreHoldsNoClusterAnswer(t *testing.T) {
	s := committedCommonStore(t)
	for _, f := range s.Fixtures() {
		out, _, _ := s.Lookup(f.Signature)
		if why := clusterAnswer(out); why != "" {
			t.Errorf("%s: %s; the common store records kubectl version --client", f.Signature, why)
		}
	}
}
