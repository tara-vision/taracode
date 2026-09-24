package evals

import (
	"os"
	"path/filepath"
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
func isolateEnv() (restore func(), err error) {
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
