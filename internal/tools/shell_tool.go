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
	"slices"
	"strings"
	"time"
	"unicode"

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
		Description: "Run a shell command in the working directory. Read-only commands (cat, grep, ps, df, dig, jq, date, " +
			"kubectl get, git status, ...) work in investigate mode; anything else needs operate mode.",
		Params: []Param{
			{Name: "command", Type: "string", Description: "The command line", Required: true},
			{Name: "timeout", Type: "integer", Description: "Seconds before the command is killed (default 60, max 600)"},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			command := argString(args, "command")
			res := classify.Shell(command)
			glob := shellGlob(dotglobOn(command))
			targets := policy.Targets{Hosts: res.Hosts, Named: shellTargets(res.Named, workingDir, glob),
				Paths: slices.Concat(shellTargets(res.Paths, workingDir, glob), landingTargets(res.Landings, workingDir, glob)),
				Finds: findStarts(res.Finds, workingDir)}
			if res.Classification == policy.Mutate && res.Opaque {
				targets.Named = append(targets.Named, shellTargets(lineTokens(command), workingDir, nil)...)
			}
			if res.Classification == policy.Mutate && len(res.Kube) > 0 {
				targets.KubeContext, targets.KubeNamespace, targets.KubeReason =
					shellKubeTargets(context.Background(), workingDir, res.Kube)
			}
			return policy.Invocation{Tool: "shell", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: command, Lines: res.Lines, Targets: targets}
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

// lineTokens are the words of an opaque line that may name a file: the line split at whitespace, quotes,
// brackets and the shell's operators, without its options.
func lineTokens(command string) []string {
	var out []string
	for _, token := range strings.FieldsFunc(command, tokenSeparator) {
		if !strings.HasPrefix(token, "-") {
			out = append(out, token)
		}
	}
	return out
}

func tokenSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'`()[]{},;=<>|&+\\", r)
}

// globFunc expands a path glob to the files it matches now.
type globFunc func(pattern string) []string

// shellGlob expands a glob as the shell does: a * or ? does not match the leading dot of a name unless the
// pattern's element starts with a dot or the line turns dotglob on.
func shellGlob(dotglob bool) globFunc {
	return func(pattern string) []string {
		matches, _ := filepath.Glob(pattern)
		if dotglob {
			return matches
		}
		elems := strings.Split(filepath.ToSlash(pattern), "/")
		var out []string
		for _, m := range matches {
			if !dotHidden(elems, strings.Split(filepath.ToSlash(m), "/")) {
				out = append(out, m)
			}
		}
		return out
	}
}

// dotHidden reports a match the shell's glob leaves out: an element that starts with a dot where the pattern's
// element does not.
func dotHidden(pattern, match []string) bool {
	for i := range min(len(pattern), len(match)) {
		if strings.HasPrefix(match[i], ".") && !strings.HasPrefix(pattern[i], ".") {
			return true
		}
	}
	return false
}

// dotglobOn reports a line that turns on matching dot files (bash's dotglob or GLOBIGNORE, zsh's globdots).
func dotglobOn(command string) bool {
	folded := strings.ToLower(strings.ReplaceAll(command, "_", ""))
	return strings.Contains(folded, "dotglob") || strings.Contains(folded, "globdots") ||
		strings.Contains(folded, "globignore")
}

// shellTargets resolves the files a shell line acts on: a leading ~, $HOME or ${HOME} to the home
// directory, a relative path against the working directory, and, given a glob function, a glob to
// itself and to the files it matches now. The words of an opaque line are resolved without one: a * in
// a program's text is no file name.
func shellTargets(paths []string, workingDir string, glob globFunc) []string {
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
		if glob != nil && strings.ContainsAny(p, "*?[") {
			for _, m := range glob(abs) {
				add(m)
			}
		}
	}
	return out
}

// landingTargets resolves where a copy, move or link puts each source, as shellTargets resolves a path, and
// keeps a landing under each directory its destination names now, a glob expanded as the shell expands it: a
// destination that is no directory is written by its own name.
func landingTargets(landings []string, workingDir string, glob globFunc) []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, landing := range landings {
		abs := resolvePath(expandHome(landing, home), workingDir)
		dirs := []string{filepath.Dir(abs)}
		if strings.ContainsAny(dirs[0], "*?[") {
			dirs = glob(dirs[0])
		}
		for _, dir := range dirs {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				out = append(out, shellTargets([]string{filepath.Join(dir, filepath.Base(abs))}, "", glob)...)
			}
		}
	}
	return out
}

// findStarts resolves the start point of each find a shell line runs as shellTargets resolves a path; the
// start point as written stays, since it begins every path find prints.
func findStarts(finds []policy.Find, workingDir string) []policy.Find {
	home, _ := os.UserHomeDir()
	out := make([]policy.Find, 0, len(finds))
	for _, f := range finds {
		found := f
		found.Start = resolvePath(expandHome(f.Start, home), workingDir)
		out = append(out, found)
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

// causeShellExpansion is why a context or namespace the shell expands before kubectl or helm reads
// it is "*".
const causeShellExpansion = "a context or namespace word is expanded by the shell"

// shellKubeTargets resolves the clusters the kubectl and helm mutations of a shell line act on, as
// the kubectl and helm tools do (what a command does not name is HELM_KUBECONTEXT or HELM_NAMESPACE
// for helm, then the current context or namespace of its kubeconfig, each kubeconfig read once), and
// reports "*" for a value the shell computes or expands at run time or when the line names more than
// one context or namespace.
func shellKubeTargets(ctx context.Context, workingDir string, refs []classify.KubeTarget) (
	kubeContext, namespace, reason string,
) {
	resolver := newKubeResolver(ctx, workingDir)
	contexts := make([]string, 0, len(refs))
	namespaces := make([]string, 0, len(refs))
	for _, ref := range refs {
		c, ns, cause := runTimeAsAll(ref.Context), runTimeAsAll(ref.Namespace), ref.Cause
		if cause == "" && (c != ref.Context || ns != ref.Namespace) {
			cause = causeShellExpansion
		}
		if ref.Helm {
			c, ns = helmEnvTargets(c, ns)
		}
		t := resolver.targets(c, ns, ref.Kubeconfig, cause)
		contexts = append(contexts, t.KubeContext)
		namespaces = append(namespaces, t.KubeNamespace)
		if reason == "" {
			reason = t.KubeReason
		}
	}
	return oneOrAll(contexts), oneOrAll(namespaces), reason
}

// runTimeAsAll maps a value the shell computes or expands at run time to "*", since it can be any: a
// $CTX or a backtick, and a glob or a brace (sh expands kube-sys{tem,} to kube-system kube-sys, and
// kube-syst* to the files it matches in the working directory). The classifier's words have their
// quotes removed, so a quoted glob or brace, which sh leaves alone, is "*" too: that fails closed, and
// no namespace can hold these characters.
func runTimeAsAll(value string) string {
	if strings.ContainsAny(value, "$`*?[{") {
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
