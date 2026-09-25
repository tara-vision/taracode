package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func TestWriteAndReadResults(t *testing.T) {
	dir := t.TempDir()
	r := Results{Taracode: "3.0.0-beta.1", Model: "qwen3.8:27b", Tier: "32", Date: "2026-09-26",
		Tasks:   []TaskResult{{ID: "a", Area: "kubernetes", Score: 0.9, Pass: true}},
		Summary: Summary{PassRate: 1, MeanScore: 0.9, ByArea: map[string]AreaSummary{"kubernetes": {Tasks: 1, Passed: 1, MeanScore: 0.9}}}}
	path, err := WriteResults(dir, r)
	if err != nil || filepath.Base(path) != "qwen3.8-27b-2026-09-26.json" {
		t.Fatalf("%q %v", path, err)
	}
	all, err := ReadResults(dir)
	if err != nil || len(all) != 1 || all[0].Model != "qwen3.8:27b" || all[0].Tasks[0].Score != 0.9 {
		t.Fatalf("%+v %v", all, err)
	}
	if ModelSlug("hf.co/org/model:Q4") != "hf.co-org-model-q4" {
		t.Fatal(ModelSlug("hf.co/org/model:Q4"))
	}
}

// TestReadResultsSkipsPartialFiles: a "-partial.json" file in the results directory (it belongs in the
// runs directory, never here, but this is defense in depth) is never read back as a committed result.
func TestReadResultsSkipsPartialFiles(t *testing.T) {
	dir := t.TempDir()
	r := Results{Model: "gemma4:12b", Date: "2026-09-26",
		Tasks:   []TaskResult{{ID: "a", Area: "kubernetes", Score: 0.9, Pass: true}},
		Summary: Summary{PassRate: 1, MeanScore: 0.9, ByArea: map[string]AreaSummary{}}}
	if _, err := WriteResults(dir, r); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, ModelSlug(r.Model)+"-"+r.Date+"-partial.json")
	if err := os.WriteFile(partial, []byte(`{"model":"gemma4:12b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := ReadResults(dir)
	if err != nil || len(all) != 1 || all[0].Model != "gemma4:12b" {
		t.Fatalf("all=%+v err=%v", all, err)
	}
}

func TestSummarizeWeightsTheScoreAndCountsAreas(t *testing.T) {
	tasks := []Task{{ID: "a", Weight: 3}, {ID: "b", Weight: 1}}
	s := summarize(tasks, []TaskResult{
		{ID: "a", Area: "kubernetes", Score: 1, Pass: true, Iterations: 4, WallMs: 100, ToolCalls: 3, FixtureMisses: 1},
		{ID: "b", Area: "helm", Score: 0.2, Iterations: 2, WallMs: 300, ToolCalls: 1, SafetyFailure: true},
	})
	if s.MeanScore != 0.8 || s.PassRate != 0.5 || s.MeanIterations != 3 || s.MeanWallMs != 200 ||
		s.FixtureMissRate != 0.25 || s.SafetyFailures != 1 {
		t.Fatalf("%+v", s)
	}
	if a := s.ByArea["kubernetes"]; a.Tasks != 1 || a.Passed != 1 || a.MeanScore != 1 {
		t.Errorf("kubernetes %+v", a)
	}
	if a := s.ByArea["helm"]; a.Tasks != 1 || a.Passed != 0 || a.MeanScore != 0.2 {
		t.Errorf("helm %+v", a)
	}
	if empty := summarize(nil, nil); empty.ByArea == nil || empty.PassRate != 0 || empty.MeanScore != 0 {
		t.Errorf("empty %+v", empty)
	}
}

// TestPublicErrorScrubsAddressesAndPaths covers ruling P3-R45 on its own: a URL error loses its
// operation and URL, the engine-side causes become fixed phrases, and the engine, IP addresses and
// host paths are scrubbed, while text with no address in it stays as it was.
func TestPublicErrorScrubsAddressesAndPaths(t *testing.T) {
	opts := RunOptions{Host: "http://engine.example.internal:11434", scope: &runScope{home: "/srv/operator"}}
	task := Task{ID: "crashloop-oomkilled", Dir: "/srv/operator/src/taracode/evals/tasks/crashloop-oomkilled"}
	chat := func(cause error) error {
		return fmt.Errorf("ollama: /api/chat: %w",
			&url.Error{Op: "Post", URL: "http://engine.example.internal:11434/api/chat", Err: cause})
	}
	dns := &net.DNSError{Err: "no such host", Name: "engine.example.internal"}
	tmp := filepath.Join(os.TempDir(), "taracode-eval-1")
	cases := []struct {
		err  error
		want string
	}{
		{chat(io.EOF), "ollama: /api/chat: EOF"},
		{chat(context.DeadlineExceeded), "ollama: /api/chat: the turn timed out"},
		{chat(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}),
			"ollama: /api/chat: the engine connection failed"},
		{chat(&net.OpError{Op: "dial", Net: "tcp", Err: dns}), "ollama: /api/chat: the engine name did not resolve"},
		{fmt.Errorf("chat: %w", context.DeadlineExceeded), "chat: the turn timed out"},
		{errors.New("read tcp 203.0.113.7:52100->203.0.113.9:11434: read: connection reset by peer"),
			"read tcp <addr>-><addr>: read: connection reset by peer"},
		{errors.New("dial tcp [2001:db8::1]:11434: connect: no route to host"), "dial tcp <addr>: connect: no route to host"},
		{errors.New("peer fe80::1 and ::1 went away"), "peer <addr> and <addr> went away"},
		{errors.New(`Post "http://engine.example.internal:11434/api/chat": EOF`), `Post "<engine>/api/chat": EOF`},
		{errors.New("engine.example.internal:11434 refused, engine.example.internal is down"), "<engine> refused, <engine> is down"},
		{errors.New("open " + task.Dir + "/policy.yaml: denied"), "open evals/tasks/crashloop-oomkilled/policy.yaml: denied"},
		{errors.New("read /srv/operator/.kube/config: denied"), "read ~/.kube/config: denied"},
		{errors.New("read /srv/operatorx/file: denied"), "read /srv/operatorx/file: denied"},
		{errors.New("open /home/ollama/.ollama/models/blobs/sha256-ab: denied"), "open ~/.ollama/models/blobs/sha256-ab: denied"},
		{errors.New("stat /Users/alice/.ollama, /root/.ollama and /home/bob"), "stat ~/.ollama, ~/.ollama and ~"},
		{errors.New("mount /rootfs/x and /var/home/bob/y"), "mount /rootfs/x and /var/home/bob/y"},
		{errors.New("dial ::ffff:192.0.2.10 failed"), "dial <addr> failed"},
		{errors.New("dial tcp [::ffff:192.0.2.10]:11434: refused"), "dial tcp <addr>: refused"},
		{errors.New("via 64:ff9b::192.0.2.10 and 0:0:0:0:0:ffff:192.0.2.10"), "via <addr> and <addr>"},
		{errors.New("mkdir " + tmp + ": exists"), "mkdir <tmp>/taracode-eval-1: exists"},
		{errors.New("version 0.34.2 at 10:00:00, replicas=3"), "version 0.34.2 at 10:00:00, replicas=3"},
	}
	for _, c := range cases {
		if got := publicError(c.err, opts, task); got != c.want {
			t.Errorf("publicError(%q)\n got %q\nwant %q", c.err, got, c.want)
		}
	}
	if got := publicNotes([]string{"forbidden: read_file path=" + tmp + "/x"}, opts, task); got[0] !=
		"forbidden: read_file path=<tmp>/taracode-eval-1/x" {
		t.Errorf("notes %q", got)
	}
	// A host given with doubled trailing slashes still scrubs whole: the client trims them all, so
	// the text carries http://host:port/api/chat (ruling P3-R56).
	slashed := RunOptions{Host: "http://engine.example.internal:11434//", scope: opts.scope}
	if got := publicText(`Post "http://engine.example.internal:11434/api/chat": EOF`, slashed, task); got !=
		`Post "<engine>/api/chat": EOF` {
		t.Errorf("doubled slashes: %q", got)
	}
}

// TestRunnerResultsCarryNoEngineHomePath: an engine error whose status body names the engine's own
// home (a models directory under /home/ollama) reaches the results as ~ (ruling P3-R56).
func TestRunnerResultsCarryNoEngineHomePath(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t, ollamatest.Turn{Status: http.StatusInternalServerError,
		Error: "open /home/ollama/.ollama/models/blobs/sha256-1234: permission denied"})
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err != nil {
		t.Fatal(err)
	}
	path, err := WriteResults(t.TempDir(), res)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if text := string(data); strings.Contains(text, "/home/ollama") || !strings.Contains(text, "~/.ollama/models/blobs") {
		t.Fatalf("the engine's home path:\n%s", text)
	}
}

// hostPort matches a host:port pair, the shape no results file may carry (ruling P3-R45).
var hostPort = regexp.MustCompile(`[A-Za-z0-9.-]+:[0-9]{2,5}\b`)

// TestRunnerResultsCarryNoEngineAddress is the review's probe of ruling P3-R45: the engine drops the
// task's first model request, whose error text names the engine's URL and the loopback address. The
// results file written from the run carries none of it; the terminal keeps the raw text.
func TestRunnerResultsCarryNoEngineAddress(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t, ollamatest.Turn{Content: "unused"})
	proxy := newEngineProxy(t, srv.URL, func(w http.ResponseWriter, _ *http.Request, _ string, n int) bool {
		if n != 2 { // the warm-up passes; the task's first request is dropped
			return false
		}
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
		return true
	})
	opts := runOptions(srv, "")
	opts.Host = proxy.URL
	var out bytes.Buffer
	opts.Out = &out
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks[0].Error == "" {
		t.Fatalf("the dropped request left no error: %+v", res.Tasks[0])
	}
	path, err := WriteResults(t.TempDir(), res)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if text := string(data); strings.Contains(text, proxy.URL) || strings.Contains(text, "127.0.0.1") ||
		hostPort.MatchString(text) {
		t.Fatalf("the results carry an engine address:\n%s", text)
	}
	if !strings.Contains(out.String(), proxy.URL) {
		t.Errorf("the terminal lost the raw error: %q", out.String())
	}
}

// TestRunnerResultsCarryNoHostPath: a setup failure whose error names host paths (copyDir refuses a
// symlink under the task's workdir) reaches the results without the task directory or the
// temporary directory (ruling P3-R45).
func TestRunnerResultsCarryNoHostPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, tasks := corpusWithTriage(t)
	dir := filepath.Join(root, "crashloop-oomkilled")
	if err := os.MkdirAll(filepath.Join(dir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "task.yaml"), filepath.Join(dir, "workdir", "task-link.yaml")); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), tasks, runOptions(fakeOllama(t), ""))
	if err == nil || !strings.Contains(err.Error(), "setup defect") {
		t.Fatalf("err=%v", err)
	}
	path, err := WriteResults(t.TempDir(), res)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	tmp := filepath.Clean(os.TempDir())
	realTmp, _ := filepath.EvalSymlinks(tmp)
	for _, hostPath := range []string{dir, root, tmp, realTmp} {
		if hostPath != "" && strings.Contains(text, hostPath) {
			t.Fatalf("the results carry %s:\n%s", hostPath, text)
		}
	}
	if !strings.Contains(text, "evals/tasks/crashloop-oomkilled/workdir/task-link.yaml") {
		t.Fatalf("the scrubbed path is missing:\n%s", text)
	}
}
