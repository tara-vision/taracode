package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
)

const (
	defaultShellTimeout = 60 * time.Second
	maxShellTimeout     = 600 * time.Second
)

// withTimeout derives a per-call context; a zero d keeps the parent's deadline.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// ShellTool runs a command line with sh -c in the working directory. stream, when set, receives the
// output live (NewBuiltinRegistry hands it a redacting one, flushed when the command ends); the
// result carries the merged output and the exit status. The invocation's targets are the hosts the
// line names, the files it writes or removes and the clusters its kubectl and helm mutations act on,
// for the protected targets.
func ShellTool(stream io.Writer) *Tool {
	return &Tool{
		Name: "shell", ReadForm: true,
		Description: "Run a shell command in the working directory. Read-only commands (cat, grep, ps, df, dig, jq, " +
			"kubectl get, git status, ...) work in investigate mode; anything else needs operate mode.",
		Params: []Param{
			{Name: "command", Type: "string", Description: "The command line", Required: true},
			{Name: "timeout", Type: "integer", Description: "Seconds before the command is killed (default 60, max 600)"},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			command := argString(args, "command")
			res := classify.Shell(command)
			targets := policy.Targets{Hosts: res.Hosts, Paths: shellTargets(res.Paths, workingDir)}
			if res.Classification == policy.Mutate && len(res.Kube) > 0 {
				targets.KubeContext, targets.KubeNamespace = shellKubeTargets(context.Background(), workingDir, res.Kube)
			}
			return policy.Invocation{Tool: "shell", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: command, Targets: targets}
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			command, err := required(args, "command")
			if err != nil {
				return "", err
			}
			timeout := time.Duration(argInt(args, "timeout", 0)) * time.Second
			if timeout <= 0 {
				timeout = defaultShellTimeout
			}
			if timeout > maxShellTimeout {
				timeout = maxShellTimeout
			}
			ctx, cancel := withTimeout(ctx, timeout)
			defer cancel()
			//nolint:gosec // the shell tool runs what the model asked for, after the policy gate
			cmd := exec.CommandContext(ctx, "sh", "-c", command)
			cmd.Dir = workingDir
			var buf bytes.Buffer
			var w io.Writer = &buf
			if stream != nil {
				w = io.MultiWriter(&buf, stream)
			}
			cmd.Stdout, cmd.Stderr = w, w
			runErr := cmd.Run()
			if f, ok := stream.(interface{ Flush() error }); ok {
				_ = f.Flush() // the live copy is best effort; the result carries the output
			}
			out := strings.TrimRight(buf.String(), "\n")
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return out, fmt.Errorf("command timed out after %s", timeout)
			}
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				return out + fmt.Sprintf("\n[exit status %d]", exitErr.ExitCode()), nil
			}
			if runErr != nil {
				return out, runErr
			}
			if out == "" {
				return "(no output)", nil
			}
			return out, nil
		},
	}
}

// shellTargets resolves the files a shell line writes or removes: a leading ~, $HOME or ${HOME} to
// the home directory, a relative path against the working directory, and a glob to itself and to
// the files it matches now.
func shellTargets(paths []string, workingDir string) []string {
	home, _ := os.UserHomeDir()
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		abs := resolvePath(expandHome(p, home), workingDir)
		add(abs)
		if strings.ContainsAny(p, "*?[") {
			matches, _ := filepath.Glob(abs)
			for _, m := range matches {
				add(m)
			}
		}
	}
	return out
}

// expandHome replaces a leading ~, $HOME or ${HOME} with home.
func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	for _, prefix := range []string{"~", "$HOME", "${HOME}"} {
		if p == prefix {
			return home
		}
		if rest, ok := strings.CutPrefix(p, prefix+"/"); ok {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// shellKubeTargets resolves the clusters the kubectl and helm mutations of a shell line act on, as
// the kubectl and helm tools do (what a command does not name is HELM_KUBECONTEXT or HELM_NAMESPACE
// for helm, then the current context or namespace of its kubeconfig, each kubeconfig read once), and
// reports "*" for a value the shell computes at run time or when the line names more than one
// context or namespace.
func shellKubeTargets(ctx context.Context, workingDir string, refs []classify.KubeTarget) (
	kubeContext, namespace string,
) {
	resolver := newKubeResolver(ctx, workingDir)
	contexts := make([]string, 0, len(refs))
	namespaces := make([]string, 0, len(refs))
	for _, ref := range refs {
		c, ns := runTimeAsAll(ref.Context), runTimeAsAll(ref.Namespace)
		if ref.Helm {
			c, ns = helmEnvTargets(c, ns)
		}
		t := resolver.targets(c, ns, ref.Kubeconfig)
		contexts = append(contexts, t.KubeContext)
		namespaces = append(namespaces, t.KubeNamespace)
	}
	return oneOrAll(contexts), oneOrAll(namespaces)
}

// runTimeAsAll maps a value the shell computes at run time ($CTX) to "*": it can be any.
func runTimeAsAll(value string) string {
	if strings.ContainsAny(value, "$`") {
		return "*"
	}
	return value
}

// oneOrAll is the value every entry shares, or "*" when they differ.
func oneOrAll(values []string) string {
	for _, v := range values[1:] {
		if v != values[0] {
			return "*"
		}
	}
	return values[0]
}
