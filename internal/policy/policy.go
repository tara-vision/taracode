// Package policy holds the rules a tool call must pass before it may change anything: the operating
// mode, the per-invocation classification and targets, the YAML policy with its protected targets,
// deny patterns and dry-run requirements, and the permission store. It imports nothing else from
// taracode, so the tools package classifies into its types and the agent loop evaluates them.
package policy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode is the operating mode of a session.
type Mode string

// The two operating modes.
const (
	ModeInvestigate Mode = "investigate"
	ModeOperate     Mode = "operate"
)

// ParseMode accepts the two mode names, case-insensitively.
func ParseMode(s string) (Mode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "investigate":
		return ModeInvestigate, true
	case "operate":
		return ModeOperate, true
	}
	return "", false
}

// Classification is what one tool invocation does.
type Classification string

// The two classifications a tool invocation can carry.
const (
	Read   Classification = "read"
	Mutate Classification = "mutate"
)

// Targets are the things a mutate invocation touches; the protected lists match them.
type Targets struct {
	KubeContext   string
	KubeNamespace string
	CloudAccount  string
	Paths         []string // absolute paths
	Hosts         []string
}

// Invocation is one classified tool call, the unit the policy evaluates.
type Invocation struct {
	Tool           string
	Verb           string // the effective verb (apply, upgrade, ...); "" for file tools
	Classification Classification
	Reason         string // why it was classified so, in words the model and the user read
	Command        string // the effective command line for deny patterns; "" for file tools
	WorkingDir     string // where relative protected paths are resolved
	Targets        Targets
}

// Policy is the merged rule set.
type Policy struct {
	Version       int           `yaml:"version"`
	Mode          Mode          `yaml:"mode"`
	Protected     Protected     `yaml:"protected"`
	Deny          DenyRules     `yaml:"deny"`
	RequireDryRun RequireDryRun `yaml:"require_dry_run"`
	Redact        Redact        `yaml:"redact"`
}

// Protected lists targets that operate mode may never mutate. Patterns are globs.
type Protected struct {
	KubeContexts   []string `yaml:"kube_contexts"`
	KubeNamespaces []string `yaml:"kube_namespaces"`
	CloudAccounts  []string `yaml:"cloud_accounts"`
	Paths          []string `yaml:"paths"`
	Hosts          []string `yaml:"hosts"`
}

// DenyRules lists command globs that are refused outright.
type DenyRules struct {
	Commands []string `yaml:"commands"`
}

// RequireDryRun switches the mandatory dry runs; nil means the default (on).
type RequireDryRun struct {
	KubectlApply   *bool `yaml:"kubectl_apply"`
	TerraformApply *bool `yaml:"terraform_apply"`
	HelmUpgrade    *bool `yaml:"helm_upgrade"`
}

// Redact configures output redaction; nil Enabled means on.
type Redact struct {
	Enabled       *bool    `yaml:"enabled"`
	ExtraPatterns []string `yaml:"extra_patterns"`
}

// CurrentVersion is the only policy file version this build reads.
const CurrentVersion = 1

// Default is the built-in policy, in force when no policy file exists.
func Default() Policy {
	on := true
	return Policy{
		Version: CurrentVersion,
		Mode:    ModeInvestigate,
		Protected: Protected{
			KubeContexts:   []string{"*prod*", "*production*"},
			KubeNamespaces: []string{"kube-system"},
			Paths:          []string{"**/*.tfstate", ".git/**"},
		},
		Deny:          DenyRules{Commands: []string{"rm -rf /*", "kubectl delete namespace *", "terraform destroy*"}},
		RequireDryRun: RequireDryRun{KubectlApply: &on, TerraformApply: &on, HelmUpgrade: &on},
		Redact:        Redact{Enabled: &on},
	}
}

// Parse decodes one policy file. Unknown keys are errors so a typo cannot silently drop a rule.
func Parse(data []byte) (Policy, error) {
	var p Policy
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return Policy{}, errors.New("policy file is empty")
		}
		return Policy{}, err
	}
	if p.Version != CurrentVersion {
		return Policy{}, fmt.Errorf("policy version %d is not supported (this build reads version %d)",
			p.Version, CurrentVersion)
	}
	if p.Mode != "" {
		mode, ok := ParseMode(string(p.Mode))
		if !ok {
			return Policy{}, fmt.Errorf("policy mode %q is not investigate or operate", p.Mode)
		}
		p.Mode = mode
	}
	return p, nil
}

// Merge lays over on top of base: lists are unioned in order, the mode is over's when set, and a
// boolean that is true on either side stays true (the stricter value).
func Merge(base, over Policy) Policy {
	out := base
	if over.Version > out.Version {
		out.Version = over.Version
	}
	if over.Mode != "" {
		out.Mode = over.Mode
	}
	out.Protected = Protected{
		KubeContexts:   union(base.Protected.KubeContexts, over.Protected.KubeContexts),
		KubeNamespaces: union(base.Protected.KubeNamespaces, over.Protected.KubeNamespaces),
		CloudAccounts:  union(base.Protected.CloudAccounts, over.Protected.CloudAccounts),
		Paths:          union(base.Protected.Paths, over.Protected.Paths),
		Hosts:          union(base.Protected.Hosts, over.Protected.Hosts),
	}
	out.Deny.Commands = union(base.Deny.Commands, over.Deny.Commands)
	out.RequireDryRun = RequireDryRun{
		KubectlApply:   stricter(base.RequireDryRun.KubectlApply, over.RequireDryRun.KubectlApply),
		TerraformApply: stricter(base.RequireDryRun.TerraformApply, over.RequireDryRun.TerraformApply),
		HelmUpgrade:    stricter(base.RequireDryRun.HelmUpgrade, over.RequireDryRun.HelmUpgrade),
	}
	out.Redact = Redact{
		Enabled:       stricter(base.Redact.Enabled, over.Redact.Enabled),
		ExtraPatterns: union(base.Redact.ExtraPatterns, over.Redact.ExtraPatterns),
	}
	return out
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, v := range list {
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// stricter keeps true when either side says true; nil means not set.
func stricter(a, b *bool) *bool {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	v := *a || *b
	return &v
}

// Files returns the global and project policy file paths.
func Files(projectDir, homeDir string) (global, project string) {
	return filepath.Join(homeDir, ".taracode", "policy.yaml"), filepath.Join(projectDir, ".taracode", "policy.yaml")
}

// Load reads the global and the project policy files, merges them (project over global) and adds the
// built-in protections. Without any file it returns Default(). sources names what was used.
func Load(projectDir, homeDir string) (Policy, []string, error) {
	globalPath, projectPath := Files(projectDir, homeDir)
	var merged Policy
	var sources []string
	for _, path := range []string{globalPath, projectPath} {
		data, err := os.ReadFile(path) //nolint:gosec // the two policy file locations are fixed by taracode
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Policy{}, nil, fmt.Errorf("read %s: %w", path, err)
		}
		p, err := Parse(data)
		if err != nil {
			return Policy{}, nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(sources) == 0 {
			merged = p
		} else {
			merged = Merge(merged, p)
		}
		sources = append(sources, path)
	}
	if len(sources) == 0 {
		merged = Default()
		sources = []string{"built-in"}
	}
	merged.Protected.Paths = union(merged.Protected.Paths, []string{globalPath, projectPath})
	return merged, sources, nil
}

// RedactEnabled is the effective redaction switch.
func (p Policy) RedactEnabled() bool { return enabled(p.Redact.Enabled) }

func enabled(b *bool) bool { return b == nil || *b }
