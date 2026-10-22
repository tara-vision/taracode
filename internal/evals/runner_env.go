package evals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// isolatedEnv are the variables Run replaces for its own duration. The kubectl, helm and shell
// classifiers resolve a context or namespace a mutation does not name through KUBECONFIG and the
// helm variables (ruling P3-R29); HOME holds the global policy, the default kubeconfig and what a
// leading ~ expands to (ruling P3-R46).
var isolatedEnv = []string{"KUBECONFIG", "HELM_KUBECONTEXT", "HELM_NAMESPACE", "HOME"}

// isolateEnv makes a run independent of the host: KUBECONFIG names a file that does not exist
// inside a fresh temporary directory, HOME is an empty directory beside it, and HELM_KUBECONTEXT and
// HELM_NAMESPACE are cleared. An unnamed context or namespace then resolves to "*" on every host,
// which the policy denies only when it protects contexts or namespaces, and no eval reads the
// host's global policy or kubeconfig. restore puts the variables back as they were and removes the
// directory; Run is not concurrent with anything that reads them.
func isolateEnv(live bool) (restore func(), err error) {
	if live { // a live run works against the host's kubeconfig and home: they are the sandbox
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "taracode-eval-env-*")
	if err != nil {
		return nil, err
	}
	type saved struct {
		value string
		set   bool
	}
	before := make(map[string]saved, len(isolatedEnv))
	for _, name := range isolatedEnv {
		value, set := os.LookupEnv(name)
		before[name] = saved{value: value, set: set}
	}
	restore = func() {
		for _, name := range isolatedEnv {
			if s := before[name]; s.set {
				_ = os.Setenv(name, s.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
		_ = os.RemoveAll(dir)
	}
	home := filepath.Join(dir, "home")
	err = os.Mkdir(home, 0o700)
	if err == nil {
		err = os.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig")) // never created
	}
	if err == nil {
		err = os.Setenv("HOME", home)
	}
	if err == nil {
		err = os.Unsetenv("HELM_KUBECONTEXT")
	}
	if err == nil {
		err = os.Unsetenv("HELM_NAMESPACE")
	}
	if err != nil {
		restore()
		return nil, err
	}
	return restore, nil
}

// checkLiveRun is the fail-closed gate of a live run, before anything runs: a live run needs the sandbox label
// and the matching kube context and executes only live tasks; a replay run refuses a live task.
func checkLiveRun(tasks []Task, opts RunOptions) error {
	if opts.Live && opts.Sandbox == "" {
		return errors.New("a live run executes every tool for real wherever the environment points: set " +
			"TARACODE_EVALS_LIVE_SANDBOX to the name of the sandbox this machine is, and only there")
	}
	for _, t := range tasks {
		switch {
		case !opts.Live && t.Provenance == ProvenanceLive:
			return fmt.Errorf("task %s has provenance live: its tools run for real, which needs --live", t.ID)
		case opts.Live && t.Provenance != ProvenanceLive:
			return fmt.Errorf("task %s has provenance %s: a live run executes only live tasks, never the "+
				"board's corpus for real", t.ID, t.Provenance)
		}
	}
	if opts.Live {
		return checkSandboxContext(opts.Sandbox)
	}
	return nil
}

// checkSandboxContext refuses a live run whose kubeconfig points anywhere but the labelled sandbox: when
// kubectl is on PATH, its current context must equal the label. Without kubectl (a Docker-only sandbox)
// the label alone stands.
func checkSandboxContext(label string) error {
	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, kubectl, "config", "current-context").Output() //nolint:gosec // kubectl from PATH
	current := strings.TrimSpace(string(out))
	if err != nil || current == "" {
		return fmt.Errorf("a live run needs the kubeconfig's current context to be the sandbox %q, and kubectl "+
			"reports none", label)
	}
	if current != label {
		return fmt.Errorf("a live run needs the kubeconfig's current context to be the sandbox %q, not %q", label, current)
	}
	return nil
}
