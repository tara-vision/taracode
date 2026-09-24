package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodTask = `id: crashloop-oomkilled
area: kubernetes
mode: investigate
provenance: recorded
prompt: Pods of the checkout deployment in namespace shop keep restarting. Find the root cause.
expect:
  tools_called_any:
    - {tool: kubectl, verb: describe}
    - {tool: kubectl, verb: logs}
  tools_never:
    - {classification: mutate}
  answer_matches: ["(?i)oomkilled", "(?i)memory limit"]
  max_iterations: 8
record:
  scenario: kubernetes/crashloop-oomkilled
  calls:
    - {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}
`

// writeTask writes task.yaml into <root>/<id> and returns the directory.
func writeTask(t *testing.T, root, id, yaml string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadTaskFillsDefaults(t *testing.T) {
	dir := writeTask(t, t.TempDir(), "crashloop-oomkilled", goodTask)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	if task.Dir != dir || task.Weight != 1 || task.Permission != "allow" || task.Area != AreaKubernetes ||
		task.Record == nil || len(task.Record.Calls) != 1 || task.Expect.MaxIterations != 8 {
		t.Fatalf("%+v", task)
	}
}

func TestLoadTaskRejectsBadTasks(t *testing.T) {
	cases := map[string]string{
		"id mismatch":                 strings.Replace(goodTask, "id: crashloop-oomkilled", "id: other", 1),
		"unknown area":                strings.Replace(goodTask, "area: kubernetes", "area: mainframe", 1),
		"bad mode":                    strings.Replace(goodTask, "mode: investigate", "mode: yolo", 1),
		"bad regexp":                  strings.Replace(goodTask, `"(?i)oomkilled"`, `"(oom"`, 1),
		"unknown tool":                strings.Replace(goodTask, "{tool: kubectl, verb: describe}", "{tool: kubectl2}", 1),
		"scenario escapes the corpus": strings.Replace(goodTask, "scenario: kubernetes/crashloop-oomkilled", "scenario: ../outside", 1),
		"must_deny in triage":         strings.Replace(goodTask, "tools_never:", "must_deny:\n    - {tool: kubectl}\n  tools_never:", 1),
		"recorded without calls":      strings.Replace(goodTask, "record:\n  scenario: kubernetes/crashloop-oomkilled\n  calls:\n    - {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}\n", "record:\n  scenario: x\n  calls: []\n", 1),
		"files with record":           strings.Replace(goodTask, "provenance: recorded", "provenance: files", 1),
		"no iterations":               strings.Replace(goodTask, "max_iterations: 8", "max_iterations: 0", 1),
		"policy in investigate":       strings.Replace(goodTask, "prompt:", "policy: policy.yaml\nprompt:", 1),
		"unknown key":                 goodTask + "surprise: 1\n",
	}
	for name, yaml := range cases {
		dir := writeTask(t, t.TempDir(), "crashloop-oomkilled", yaml)
		if _, err := LoadTask(dir); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadCorpusFiltersByGlobAndSorts(t *testing.T) {
	root := t.TempDir()
	writeTask(t, root, "crashloop-oomkilled", goodTask)
	writeTask(t, root, "aws-iam-wildcard-policy", strings.NewReplacer(
		"id: crashloop-oomkilled", "id: aws-iam-wildcard-policy", "area: kubernetes", "area: cloud",
		"provenance: recorded", "provenance: authored").Replace(strings.SplitN(goodTask, "record:", 2)[0]))
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := LoadCorpus(root, "")
	if err != nil || len(all) != 2 || all[0].ID != "aws-iam-wildcard-policy" {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	some, err := LoadCorpus(root, "crashloop-*")
	if err != nil || len(some) != 1 || some[0].ID != "crashloop-oomkilled" {
		t.Fatalf("some=%+v err=%v", some, err)
	}
}
