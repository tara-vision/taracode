package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

const (
	// kubeResolveTimeout bounds each kubectl config call of a resolution.
	kubeResolveTimeout = 3 * time.Second
	// maxKubeconfigSize is the size a kubeconfig a command names must stay under to be read.
	maxKubeconfigSize = 1 << 20
)

// kubeResolver fills in what a kubectl or helm mutation does not name, the current context and the
// default namespace of the kubeconfig it reads, with kubectl config. Only mutate invocations pay
// this cost, and one resolver serves one classification: each kubeconfig is read once, however many
// commands of a shell line use it. "*" stands for what it cannot establish: a kubeconfig known only
// at run time, one that is not a regular file under 1 MiB (a device, a FIFO or a huge file could
// stall or flood kubectl), kubectl missing, failing or timing out, or no current context.
type kubeResolver struct {
	ctx        context.Context
	workingDir string
	current    map[string]lookup    // the current context, by kubeconfig
	namespaces map[[2]string]string // the default namespace, by kubeconfig and named context
}

type lookup struct {
	value string
	ok    bool
}

func newKubeResolver(ctx context.Context, workingDir string) *kubeResolver {
	return &kubeResolver{ctx: ctx, workingDir: workingDir, current: map[string]lookup{},
		namespaces: map[[2]string]string{}}
}

// targets fills the context and namespace a command does not name ("") from kubeconfig, the
// kubeconfig it names ("" for the one taracode's environment gives kubectl). The namespace of a
// named context is that context's default namespace, not the current one's.
func (r *kubeResolver) targets(kubeContext, namespace, kubeconfig string) policy.Targets {
	t := policy.Targets{KubeContext: kubeContext, KubeNamespace: namespace}
	if t.KubeContext != "" && t.KubeNamespace != "" {
		return t
	}
	env, ok := r.kubeconfigEnv(kubeconfig)
	if !ok || t.KubeContext == "*" {
		return unresolved(t)
	}
	named := t.KubeContext
	if named == "" {
		current, ok := r.currentContext(kubeconfig, env)
		if !ok {
			return unresolved(t)
		}
		t.KubeContext = current
	}
	if t.KubeNamespace == "" {
		t.KubeNamespace = r.namespace(kubeconfig, env, named)
	}
	return t
}

// unresolved makes the context and namespace a command does not name "*".
func unresolved(t policy.Targets) policy.Targets {
	if t.KubeContext == "" {
		t.KubeContext = "*"
	}
	if t.KubeNamespace == "" {
		t.KubeNamespace = "*"
	}
	return t
}

// kubeconfigEnv is the environment that points kubectl at the kubeconfig a command names: each path
// of the list, a leading ~, $HOME or ${HOME} expanded and a relative one resolved against the
// working directory, must be a regular file under 1 MiB. ok is false for any other path, for a path
// computed at run time and for "*" (a command that names two different kubeconfigs).
func (r *kubeResolver) kubeconfigEnv(kubeconfig string) (env []string, ok bool) {
	if kubeconfig == "" {
		return nil, true
	}
	if kubeconfig == "*" {
		return nil, false
	}
	home, _ := os.UserHomeDir()
	parts := strings.Split(kubeconfig, string(os.PathListSeparator))
	paths := make([]string, 0, len(parts))
	for _, p := range parts {
		expanded := expandHome(p, home)
		if strings.ContainsAny(expanded, "$`") {
			return nil, false
		}
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(r.workingDir, expanded)
		}
		if info, err := os.Stat(expanded); err != nil || !info.Mode().IsRegular() || info.Size() >= maxKubeconfigSize {
			return nil, false
		}
		paths = append(paths, expanded)
	}
	return []string{"KUBECONFIG=" + strings.Join(paths, string(os.PathListSeparator))}, true
}

// currentContext is the current context of a kubeconfig, read once per resolver.
func (r *kubeResolver) currentContext(kubeconfig string, env []string) (string, bool) {
	if l, seen := r.current[kubeconfig]; seen {
		return l.value, l.ok
	}
	out, err := r.kubectl(env, "config", "current-context")
	l := lookup{value: out, ok: err == nil && out != ""}
	r.current[kubeconfig] = l
	return l.value, l.ok
}

// namespace is the default namespace of the named context (the current one when named is ""), read
// once per resolver: "default" when the context sets none, "*" when kubectl cannot say.
func (r *kubeResolver) namespace(kubeconfig string, env []string, named string) string {
	key := [2]string{kubeconfig, named}
	if ns, seen := r.namespaces[key]; seen {
		return ns
	}
	args := []string{"config", "view", "--minify", "-o", "jsonpath={.contexts[0].context.namespace}"}
	if named != "" {
		args = append(args, "--context", named)
	}
	out, err := r.kubectl(env, args...)
	switch {
	case err != nil:
		out = "*"
	case out == "":
		out = "default"
	}
	r.namespaces[key] = out
	return out
}

// kubectl runs one kubectl config read. It reads the kubeconfig, not project files, so a working
// directory that does not exist must not fail it: the process's own directory is used instead.
func (r *kubeResolver) kubectl(env []string, args ...string) (string, error) {
	ctx, cancel := withTimeout(r.ctx, kubeResolveTimeout)
	defer cancel()
	dir := r.workingDir
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = ""
	}
	return runOutput(ctx, dir, env, "kubectl", args...)
}

// helmEnvTargets fills the context and namespace a helm command does not name with HELM_KUBECONTEXT
// and HELM_NAMESPACE from taracode's environment, which helm reads when no flag names them (an
// empty value is unset).
func helmEnvTargets(kubeContext, namespace string) (string, string) {
	if kubeContext == "" {
		kubeContext = os.Getenv("HELM_KUBECONTEXT")
	}
	if namespace == "" {
		namespace = os.Getenv("HELM_NAMESPACE")
	}
	return kubeContext, namespace
}
