package evals

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newRecordingTask writes a scenario whose setup.sh does nothing and whose teardown.sh leaves a
// teardown-ran marker in the scenario directory, plus a task recording calls against it (YAML list
// items, each one after the first indented four spaces), and returns the loaded task, the scenarios
// root and the scenario directory.
func newRecordingTask(t *testing.T, scenarioName, calls string) (task Task, scenarios, scenario string) {
	t.Helper()
	root := t.TempDir()
	scenarios = filepath.Join(root, "scenarios")
	scenario = filepath.Join(scenarios, "kubernetes", scenarioName)
	if err := os.MkdirAll(scenario, 0o755); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"setup.sh":    "#!/bin/sh\nexit 0\n",
		"teardown.sh": "#!/bin/sh\nprintf 'ran' > \"$EVAL_SCENARIO/teardown-ran\"\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(scenario, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	taskYAML := strings.NewReplacer("kubernetes/crashloop-oomkilled", "kubernetes/"+scenarioName,
		"- {tool: kubectl, args: {verb: get, resource: pods, namespace: shop}}", calls).Replace(goodTask)
	task, err := LoadTask(writeTask(t, filepath.Join(root, "tasks"), "crashloop-oomkilled", taskYAML))
	if err != nil {
		t.Fatal(err)
	}
	return task, scenarios, scenario
}

// TestRecordTaskRefusesAPrivateNameInTheSignatureOfADirectSave is fix round 4's regression test: a
// dry run of a tool with no DryRun (shell) never reaches the middleware, so recordOneCall saves its
// result itself, and that save once skipped the private-name check on the call's signature, writing
// "dryrun:shell warehouse-42-probe" into the fixtures. It must refuse exactly as the middleware does
// for an ordinary call (the control row): an ErrFixtureHostname, no fixtures/index.yaml and no
// temporary store left behind, the refused call named in the log, and the teardown still run.
func TestRecordTaskRefusesAPrivateNameInTheSignatureOfADirectSave(t *testing.T) {
	cases := []struct{ name, call, sig string }{
		{"dry run of a tool with no DryRun", `- {tool: shell, dry_run: true, args: {command: "warehouse-42-probe"}}`,
			"dryrun:shell warehouse-42-probe"},
		{"ordinary call", `- {tool: shell, args: {command: "echo warehouse-42-probe"}}`,
			"shell echo warehouse-42-probe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(privateNamesEnvVar, "warehouse-42-probe")
			task, scenarios, scenario := newRecordingTask(t, "probe", tc.call)
			var log strings.Builder
			err := RecordTask(context.Background(), task, scenarios, nil, &log)
			if !errors.Is(err, ErrFixtureHostname) {
				t.Fatalf("expected an ErrFixtureHostname, got %v\n%s", err, log.String())
			}
			if _, statErr := os.Stat(filepath.Join(task.Dir, "fixtures", "index.yaml")); !os.IsNotExist(statErr) {
				t.Fatalf("a refused recording must leave no fixtures/index.yaml behind: %v\n%s", statErr, log.String())
			}
			if leftovers, _ := filepath.Glob(filepath.Join(task.Dir, "fixtures-recording-*")); len(leftovers) != 0 {
				t.Fatalf("a refused recording must not leave its temporary store behind: %v", leftovers)
			}
			if !regexp.MustCompile(`refused\s+` + regexp.QuoteMeta(tc.sig) + `\n`).MatchString(log.String()) {
				t.Fatalf("the log must name the refused call %q\n%s", tc.sig, log.String())
			}
			if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
				t.Fatalf("teardown did not run\n%s", log.String())
			}
		})
	}
}

// TestRecordTaskKeepsTheRecordingWhenTheFinalSwapFails drives a whole recording (fix round 4) with
// the swap's second rename forced to fail: the second call's resolver makes the temporary store's
// parent read-only once the first call has created the store's fixtures directory in it, so every
// save still works and moving the previous fixtures aside does too, but the recorded set cannot leave
// its locked parent. RecordTask must fail with that rename's error, the previous fixtures must be back
// in place, and the temporary store must survive with the whole recording in it.
func TestRecordTaskKeepsTheRecordingWhenTheFinalSwapFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions")
	}
	t.Setenv(privateNamesEnvVar, "not-a-real-name")
	task, scenarios, scenario := newRecordingTask(t, "swap-fails",
		"- {tool: shell, args: {command: \"echo first\"}}\n    - {tool: shell, args: {command: \"echo {{node}}\"}}")
	previous, err := LoadFixtures(task.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := previous.Save("shell echo previous", "previous", false); err != nil {
		t.Fatal(err)
	}
	var tempParent string
	resolve := func(context.Context, string, string) (string, error) {
		matches, err := filepath.Glob(filepath.Join(task.Dir, "fixtures-recording-*"))
		if err != nil || len(matches) != 1 {
			return "", fmt.Errorf("expected one temporary store, got %v (%v)", matches, err)
		}
		tempParent = matches[0]
		if err := os.Chmod(tempParent, 0o500); err != nil {
			return "", err
		}
		t.Cleanup(func() { _ = os.Chmod(tempParent, 0o755) }) // so t.TempDir()'s own cleanup can remove it
		return "second", nil
	}
	var log strings.Builder
	err = RecordTask(context.Background(), task, scenarios, resolve, &log)
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "move the recorded fixtures into place") {
		t.Fatalf("expected the second rename's permission error, got %v\n%s", err, log.String())
	}
	restored, err := LoadFixtures(task.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if out, _, ok := restored.Lookup("shell echo previous"); !ok || out != "previous" || restored.Len() != 1 {
		t.Fatalf("the previous fixtures must be back in place: %q %v len=%d", out, ok, restored.Len())
	}
	if _, statErr := os.Stat(filepath.Join(task.Dir, "fixtures.replaced")); !os.IsNotExist(statErr) {
		t.Fatalf("the aside copy must not remain after the rollback: %v", statErr)
	}
	kept, err := LoadFixtures(tempParent)
	if err != nil || kept.Len() != 2 {
		t.Fatalf("the temporary store must survive a failed swap with the whole recording: %v len=%d",
			err, kept.Len())
	}
	for _, sig := range []string{"shell echo first", "shell echo second"} {
		if _, _, ok := kept.Lookup(sig); !ok {
			t.Fatalf("the kept recording is missing %q\n%s", sig, log.String())
		}
	}
	if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
		t.Fatalf("teardown did not run\n%s", log.String())
	}
}

// TestRecordTaskNamesTheCallWhoseSaveFails covers recordOneCall's "save failed" branch (fix round 4),
// for a result the middleware saves and for one recordOneCall saves directly: the second call's
// resolver makes the temporary store's fixtures directory read-only once the first call has saved
// into it, so the second call's own save fails. The log must name that call's signature, and
// RecordTask must return an error wrapping the save failure.
func TestRecordTaskNamesTheCallWhoseSaveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions")
	}
	cases := []struct{ name, call, sig string }{
		{"saved by the middleware", `- {tool: shell, args: {command: "echo {{node}}"}}`, "shell echo second"},
		{"saved directly", `- {tool: shell, dry_run: true, args: {command: "echo {{node}}"}}`,
			"dryrun:shell echo second"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(privateNamesEnvVar, "not-a-real-name")
			task, scenarios, scenario := newRecordingTask(t, "save-fails",
				"- {tool: shell, args: {command: \"echo first\"}}\n    "+tc.call)
			var storeDir string
			resolve := func(context.Context, string, string) (string, error) {
				matches, err := filepath.Glob(filepath.Join(task.Dir, "fixtures-recording-*", "fixtures"))
				if err != nil || len(matches) != 1 {
					return "", fmt.Errorf("expected one temporary fixtures directory, got %v (%v)", matches, err)
				}
				storeDir = matches[0]
				if err := os.Chmod(storeDir, 0o500); err != nil {
					return "", err
				}
				t.Cleanup(func() { _ = os.Chmod(storeDir, 0o755) }) // so t.TempDir()'s own cleanup can remove it
				return "second", nil
			}
			var log strings.Builder
			err := RecordTask(context.Background(), task, scenarios, resolve, &log)
			var pathErr *fs.PathError
			if !errors.Is(err, fs.ErrPermission) || !errors.As(err, &pathErr) || filepath.Dir(pathErr.Path) != storeDir {
				t.Fatalf("expected an error wrapping the save failure in %s, got %v\n%s", storeDir, err, log.String())
			}
			if !regexp.MustCompile(`save failed\s+` + regexp.QuoteMeta(tc.sig) + `\n`).MatchString(log.String()) {
				t.Fatalf("the log must name the call whose save failed, %q\n%s", tc.sig, log.String())
			}
			if _, statErr := os.Stat(filepath.Join(task.Dir, "fixtures")); !os.IsNotExist(statErr) {
				t.Fatalf("an aborted recording must not create the real fixtures: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
				t.Fatalf("teardown did not run\n%s", log.String())
			}
		})
	}
}

// TestRecordTaskAbortsANoDryRunCallWhoseContextEnded pins what recordOneCall's fall-through gives a
// dry run answered with ErrNoDryRun (fix round 4): its branch no longer returns early, so a context
// that ends during the call aborts the run as cancelled. That holds whether the middleware skipped the
// save (kubectl, whose own DryRun refuses a get) or guardAndSave skipped the direct save (shell, with
// no DryRun at all). The call is no longer logged as recorded, and the final swap never runs.
func TestRecordTaskAbortsANoDryRunCallWhoseContextEnded(t *testing.T) {
	cases := []struct{ name, call, sig string }{
		{"save skipped by the middleware",
			`- {tool: kubectl, dry_run: true, args: {verb: get, resource: pods, namespace: "{{node}}"}}`,
			DryRunSignature("kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "second"})},
		{"direct save skipped", `- {tool: shell, dry_run: true, args: {command: "echo {{node}}"}}`,
			"dryrun:shell echo second"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(privateNamesEnvVar, "not-a-real-name")
			task, scenarios, scenario := newRecordingTask(t, "cut-short", tc.call)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resolve := func(context.Context, string, string) (string, error) {
				cancel() // the caller gives up while this call is under way
				return "second", nil
			}
			var log strings.Builder
			err := RecordTask(ctx, task, scenarios, resolve, &log)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected a context.Canceled error, got %v\n%s", err, log.String())
			}
			if !regexp.MustCompile(`cancelled\s+` + regexp.QuoteMeta(tc.sig) + `\n`).MatchString(log.String()) {
				t.Fatalf("the log must name the cancelled call %q\n%s", tc.sig, log.String())
			}
			if _, statErr := os.Stat(filepath.Join(task.Dir, "fixtures")); !os.IsNotExist(statErr) {
				t.Fatalf("a cancelled recording must not create the real fixtures: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
				t.Fatalf("teardown did not run\n%s", log.String())
			}
		})
	}
}

// TestRecordTaskResolvesRelativeRoots drives RecordTask the way `make record` does (ruling P3-R55
// item 1): a relative corpus root and a relative scenarios root, from the working directory holding
// them. sh runs a script with its scenario directory as the working directory, so a relative script
// path was once resolved from inside it and failed with exit status 127 before setup, or the
// teardown, could run. Recording must work, with EVAL_SCENARIO absolute, and a failing setup must
// still be followed by the teardown.
func TestRecordTaskResolvesRelativeRoots(t *testing.T) {
	cases := []struct{ name, setup, wantErr string }{
		{"records", "#!/bin/sh\ncase \"$EVAL_SCENARIO\" in /*) ;; *) exit 3 ;; esac\n" +
			"printf 'from setup' > \"$EVAL_WORKDIR/marker.txt\"\n", ""},
		{"setup fails", "#!/bin/sh\nprintf 'ran' > \"$EVAL_SCENARIO/setup-ran\"\nexit 1\n", "setup: exit status 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(privateNamesEnvVar, "not-a-real-name")
			task, scenarios, scenario := newRecordingTask(t, "relative",
				"- {tool: shell, args: {command: \"cat marker.txt\"}}")
			if err := os.WriteFile(filepath.Join(scenario, "setup.sh"), []byte(tc.setup), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(filepath.Dir(scenarios)) // restored when the test ends
			tasks, err := LoadCorpus("tasks", "")
			if err != nil || len(tasks) != 1 || filepath.IsAbs(tasks[0].Dir) {
				t.Fatalf("expected one task loaded from a relative corpus root: %v %v", tasks, err)
			}
			var log strings.Builder
			err = RecordTask(context.Background(), tasks[0], "scenarios", nil, &log)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("expected error %q, got %v\n%s", tc.wantErr, err, log.String())
			}
			if tc.wantErr == "" {
				s, _ := LoadFixtures(task.Dir)
				if out, isErr, ok := s.Lookup("shell cat marker.txt"); !ok || isErr || out != "from setup" {
					t.Fatalf("fixture %q %v %v\n%s", out, isErr, ok, log.String())
				}
			} else if _, statErr := os.Stat(filepath.Join(scenario, "setup-ran")); statErr != nil {
				t.Fatalf("setup itself must have run and failed, not been missed\n%s", log.String())
			}
			if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
				t.Fatalf("teardown did not run\n%s", log.String())
			}
			if leftovers, _ := filepath.Glob(filepath.Join(task.Dir, "fixtures-recording-*")); len(leftovers) != 0 {
				t.Fatalf("the temporary store must not be left behind: %v", leftovers)
			}
		})
	}
}

// TestRecordTaskRefusesAPrivateNameRebuiltInTheFixtureFileName is ruling P3-R55 item 2's probe: a
// fixture's file name is a slug of its signature, so "shell echo kiosk 7" is saved as
// shell-echo-kiosk-7-<hash>.txt, and the slug rebuilds the hyphenated private name kiosk-7 that
// neither the signature nor the output ("kiosk 7") carries. The file name is checked too, on the
// middleware's save and on the direct save alike.
func TestRecordTaskRefusesAPrivateNameRebuiltInTheFixtureFileName(t *testing.T) {
	cases := []struct{ name, call, sig string }{
		{"ordinary call", `- {tool: shell, args: {command: "echo kiosk 7"}}`, "shell echo kiosk 7"},
		{"dry run of a tool with no DryRun", `- {tool: shell, dry_run: true, args: {command: "echo kiosk 7"}}`,
			"dryrun:shell echo kiosk 7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.sig, "kiosk-7") || !strings.Contains(fixtureFileName(tc.sig), "-kiosk-7-") {
				t.Fatalf("precondition: only the file name %q may carry the name", fixtureFileName(tc.sig))
			}
			t.Setenv(privateNamesEnvVar, "kiosk-7")
			task, scenarios, scenario := newRecordingTask(t, "slug", tc.call)
			var log strings.Builder
			err := RecordTask(context.Background(), task, scenarios, nil, &log)
			if !errors.Is(err, ErrFixtureHostname) {
				t.Fatalf("expected an ErrFixtureHostname, got %v\n%s", err, log.String())
			}
			if _, statErr := os.Stat(filepath.Join(task.Dir, "fixtures", "index.yaml")); !os.IsNotExist(statErr) {
				t.Fatalf("a refused recording must leave no fixtures/index.yaml behind: %v\n%s", statErr, log.String())
			}
			if leftovers, _ := filepath.Glob(filepath.Join(task.Dir, "fixtures-recording-*")); len(leftovers) != 0 {
				t.Fatalf("a refused recording must not leave its temporary store behind: %v", leftovers)
			}
			if !regexp.MustCompile(`refused\s+` + regexp.QuoteMeta(tc.sig) + `\n`).MatchString(log.String()) {
				t.Fatalf("the log must name the refused call %q\n%s", tc.sig, log.String())
			}
			if _, statErr := os.Stat(filepath.Join(scenario, "teardown-ran")); statErr != nil {
				t.Fatalf("teardown did not run\n%s", log.String())
			}
		})
	}
}

// fakeKubectl puts an executable kubectl script first on PATH for the rest of the test, the way
// internal/tools' fakeBin does.
func fakeKubectl(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRecordTaskKeepsARecordedDryRunOverAnAliasedPlaceholder pins ruling P3-R55 item 3: a shell dry
// run of "kubectl apply -f web.yaml" shares the signature of the kubectl tool's own dry run, but shell
// has no DryRun, so all it can record is the "this tool has no dry run" placeholder. Recorded after
// the real dry run, the placeholder must not overwrite the diff, and is logged as kept; recorded
// before it, the placeholder is replaced by the real dry run through Save's normal replace path.
func TestRecordTaskKeepsARecordedDryRunOverAnAliasedPlaceholder(t *testing.T) {
	const diff = "-  replicas: 2\n+  replicas: 3"
	realDryRun := `- {tool: kubectl, dry_run: true, args: {verb: apply, args: "-f web.yaml"}}`
	placeholder := `- {tool: shell, dry_run: true, args: {command: "kubectl apply -f web.yaml"}}`
	sig := DryRunSignature("kubectl", map[string]any{"verb": "apply", "args": "-f web.yaml"})
	if aliased := DryRunSignature("shell", map[string]any{"command": "kubectl apply -f web.yaml"}); aliased != sig {
		t.Fatalf("precondition: the shell dry run must alias the kubectl one: %q != %q", aliased, sig)
	}
	cases := []struct {
		name, calls string
		wantKept    bool
	}{
		{"real dry run first", realDryRun + "\n    " + placeholder, true},
		{"placeholder first", placeholder + "\n    " + realDryRun, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(privateNamesEnvVar, "not-a-real-name")
			// kubectl diff exits 1 when there are differences; the kubectl tool's dry run reads that as
			// the diff itself.
			fakeKubectl(t, "#!/bin/sh\nprintf '%s\\n' '-  replicas: 2' '+  replicas: 3'\nexit 1\n")
			task, scenarios, _ := newRecordingTask(t, "aliased", tc.calls)
			var log strings.Builder
			if err := RecordTask(context.Background(), task, scenarios, nil, &log); err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			s, _ := LoadFixtures(task.Dir)
			if out, isErr, ok := s.Lookup(sig); !ok || isErr || out != diff || s.Len() != 1 {
				t.Fatalf("the real dry run's diff must be the fixture: %q %v %v len=%d\n%s",
					out, isErr, ok, s.Len(), log.String())
			}
			kept := regexp.MustCompile(`kept\s+` + regexp.QuoteMeta(sig) +
				` \(a dry-run placeholder does not replace a recorded fixture\)\n`).MatchString(log.String())
			if kept != tc.wantKept {
				t.Fatalf("kept logged: %v, want %v\n%s", kept, tc.wantKept, log.String())
			}
		})
	}
}

// TestRunScriptResolvesARelativeScriptPath pins runScript's own half of ruling P3-R55 item 1: given a
// relative directory, it still runs the script, in that directory, although sh starts there and
// would not find the relative path from inside it.
func TestRunScriptResolvesARelativeScriptPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scenario"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'ran' > ran.txt\n"
	if err := os.WriteFile(filepath.Join(root, "scenario", "setup.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root) // restored when the test ends
	var out strings.Builder
	if _, err := runScript(context.Background(), "scenario", "setup.sh", os.Environ(), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "scenario", "ran.txt")); err != nil {
		t.Fatalf("the script must have run in its own directory: %v\n%s", err, out.String())
	}
}
