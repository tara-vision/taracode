package classify

import (
	"path"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// KubeTarget is the cluster a kubectl or helm mutation of a shell line acts on, as the line names
// it: an empty Context or Namespace is the current one of the kubeconfig, "*" is every namespace
// (-A), and Kubeconfig is the kubeconfig the command uses when the line names one.
type KubeTarget struct {
	Context, Namespace, Kubeconfig string
}

// shellKube returns the clusters the kubectl and helm mutations of a shell line act on, read from
// the tokens before "--" as the dedicated tools read them, also behind sudo, env and the other
// wrappers. The kubeconfig is a --kubeconfig flag, a KUBECONFIG prefix, or a KUBECONFIG an earlier
// segment set (KUBECONFIG=x; or export KUBECONFIG=x). A kubectl or helm read carries none: kubectl
// get -A > pods.txt writes a file, not a cluster.
func shellKube(segments []shellwords.Segment) []KubeTarget {
	var targets []KubeTarget
	lineKubeconfig := ""
	for _, seg := range segments {
		end := assignmentsEnd(seg.Words)
		kubeconfig := lineKubeconfig
		if v, ok := kubeconfigIn(seg.Words[:end]); ok {
			kubeconfig = v
			if end == len(seg.Words) {
				lineKubeconfig = v
			}
		}
		command := seg.Words[end:]
		if len(command) > 0 && command[0] == "export" {
			if v, ok := kubeconfigIn(command[1:]); ok {
				lineKubeconfig = v
			}
			continue
		}
		if t, ok := kubeCommand(command, kubeconfig); ok {
			targets = append(targets, t)
		}
	}
	return targets
}

// kubeconfigIn returns the value of the last KUBECONFIG=value among words.
func kubeconfigIn(words []string) (string, bool) {
	value, found := "", false
	for _, w := range words {
		if v, ok := strings.CutPrefix(w, "KUBECONFIG="); ok {
			value, found = v, true
		}
	}
	return value, found
}

// kubeCommand finds the kubectl or helm command of a segment, directly or behind a wrapper (whose
// KUBECONFIG= arguments, as env takes them, apply to it), and returns its target if it mutates.
func kubeCommand(words []string, kubeconfig string) (KubeTarget, bool) {
	if len(words) == 0 {
		return KubeTarget{}, false
	}
	if !wrapperPrograms[path.Base(words[0])] {
		return kubeMutation(path.Base(words[0]), words[1:], kubeconfig)
	}
	for i, w := range words[1:] {
		if v, ok := strings.CutPrefix(w, "KUBECONFIG="); ok {
			kubeconfig = v
		} else if prog := path.Base(w); prog == "kubectl" || prog == "helm" {
			return kubeMutation(prog, words[i+2:], kubeconfig)
		}
	}
	return KubeTarget{}, false
}

// kubeMutation returns the target of a kubectl or helm command that mutates; ok is false for a read
// and for any other program.
func kubeMutation(prog string, tokens []string, kubeconfig string) (KubeTarget, bool) {
	var res Result
	var t KubeTarget
	switch prog {
	case "kubectl":
		res = Kubectl(first(tokens), tail(tokens))
		t.Context, t.Namespace = KubeTargets(tokens)
	case "helm":
		res = Helm(tokens)
		t.Context, t.Namespace = HelmTargets(tokens)
	default:
		return KubeTarget{}, false
	}
	if res.Classification != policy.Mutate {
		return KubeTarget{}, false
	}
	t.Kubeconfig = kubeconfig
	if flag := KubeconfigFlag(tokens); flag != "" {
		t.Kubeconfig = flag
	}
	return t, true
}

// KubeconfigFlag is the --kubeconfig a kubectl or helm command names before "--".
func KubeconfigFlag(tokens []string) string {
	return flagValue(beforeDoubleDash(tokens), "--kubeconfig")
}
