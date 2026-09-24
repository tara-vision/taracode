// Package evals measures the taracode loop on recorded DevOps tasks. A task directory holds the
// prompt, the expectations and the fixtures its tool calls replay from; the runner drives the real
// assistant with the replay middleware, scores the answer and the calls, and the report turns
// results into the scoreboard.
package evals

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Area groups tasks on the scoreboard.
type Area string

// The corpus areas (spec 12).
const (
	AreaKubernetes Area = "kubernetes"
	AreaHelm       Area = "helm"
	AreaTerraform  Area = "terraform"
	AreaDocker     Area = "docker"
	AreaSecrets    Area = "secrets"
	AreaCloud      Area = "cloud"
	AreaRefusal    Area = "refusal"
)

var areas = map[Area]bool{AreaKubernetes: true, AreaHelm: true, AreaTerraform: true, AreaDocker: true,
	AreaSecrets: true, AreaCloud: true, AreaRefusal: true}

// Provenance says where a task's fixtures come from (ruling R7).
const (
	ProvenanceRecorded = "recorded" // the recorder wrote them from a scenario
	ProvenanceAuthored = "authored" // hand-written (cloud tasks)
	ProvenanceFiles    = "files"    // no fixtures: the task only reads its workdir
)

// MinCorpusTasks is the floor the corpus test enforces; the release task raises it to 33.
const MinCorpusTasks = 33

// Matcher selects tool calls; every given field must match. SignatureMatches is a Go regexp over
// the canonical signature (Signature).
type Matcher struct {
	Tool             string `yaml:"tool,omitempty"`
	Verb             string `yaml:"verb,omitempty"`
	Classification   string `yaml:"classification,omitempty"`
	SignatureMatches string `yaml:"signature_matches,omitempty"`
}

// Expect is what a task scores and asserts (spec 8). The tools_called lists count a call the gate
// decided, allowed or denied, except one the classifier refused (rule classifier: an argument error,
// a panicking classifier, an unknown tool), which never reached a tool or a policy decision (ruling
// P3-R65); tools_never and must_deny see every call.
type Expect struct {
	ToolsCalledAny   []Matcher `yaml:"tools_called_any"` // one matching attempt earns it; classifier refusals never count
	ToolsCalledAll   []Matcher `yaml:"tools_called_all"` // the share matched by attempts; classifier refusals never count
	ToolsNever       []Matcher `yaml:"tools_never"`      // any matching call, refused or not, zeroes the forbidden part
	MustDeny         []Matcher `yaml:"must_deny"`        // a matching call the gate allowed is a safety failure
	AnswerMatches    []string  `yaml:"answer_matches"`
	AnswerMatchesAny []string  `yaml:"answer_matches_any"`
	AnswerNever      []string  `yaml:"answer_never"`
	MaxIterations    int       `yaml:"max_iterations"`
}

// RecordCall is one call the recorder makes, written as the model would; DryRun records the tool's
// dry run instead of its execution.
type RecordCall struct {
	Tool   string         `yaml:"tool"`
	Args   map[string]any `yaml:"args"`
	DryRun bool           `yaml:"dry_run,omitempty"`
}

// Record is a recorded task's scenario and calls.
type Record struct {
	Scenario string       `yaml:"scenario"`
	Calls    []RecordCall `yaml:"calls"`
}

// Task is one task.yaml (spec 4).
type Task struct {
	ID         string  `yaml:"id"`
	Area       Area    `yaml:"area"`
	Mode       string  `yaml:"mode"`
	Provenance string  `yaml:"provenance"`
	Prompt     string  `yaml:"prompt"`
	Weight     float64 `yaml:"weight"`
	Policy     string  `yaml:"policy,omitempty"`
	Permission string  `yaml:"permission,omitempty"`
	Expect     Expect  `yaml:"expect"`
	Record     *Record `yaml:"record,omitempty"`

	Dir string `yaml:"-"` // the task directory, set by LoadTask
}

var taskID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// builtinTools are the sixteen tools a matcher or a recorded call may name.
var builtinTools = map[string]bool{
	"read_file": true, "list_files": true, "search_files": true, "write_file": true, "edit_file": true,
	"shell": true, "git": true, "kubectl": true, "helm": true, "terraform": true, "docker": true, "cloud": true,
	"scan": true, "web_search": true, "web_fetch": true, "get_datetime": true,
}

// LoadTask reads and validates <dir>/task.yaml; the directory name must equal the id.
func LoadTask(dir string) (Task, error) {
	data, err := os.ReadFile(filepath.Join(dir, "task.yaml")) //nolint:gosec // dir is a trusted corpus path
	if err != nil {
		return Task{}, err
	}
	var t Task
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return Task{}, fmt.Errorf("%s: %w", dir, err)
	}
	t.Dir = dir
	if t.Weight == 0 {
		t.Weight = 1
	}
	if t.Permission == "" {
		t.Permission = "allow"
	}
	if err := t.Validate(); err != nil {
		return Task{}, fmt.Errorf("%s: %w", dir, err)
	}
	return t, nil
}

// Validate checks the fields the runner and the scorer rely on.
func (t Task) Validate() error {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if !taskID.MatchString(t.ID) {
		fail("id %q must match %s", t.ID, taskID)
	}
	if t.Dir != "" && filepath.Base(t.Dir) != t.ID {
		fail("id %q does not match the directory %q", t.ID, filepath.Base(t.Dir))
	}
	if !areas[t.Area] {
		fail("area %q is unknown", t.Area)
	}
	if t.Mode != "investigate" && t.Mode != "operate" {
		fail("mode %q must be investigate or operate", t.Mode)
	}
	if t.Provenance != ProvenanceRecorded && t.Provenance != ProvenanceAuthored && t.Provenance != ProvenanceFiles {
		fail("provenance %q must be recorded, authored or files", t.Provenance)
	}
	if strings.TrimSpace(t.Prompt) == "" {
		fail("prompt is empty")
	}
	if t.Weight <= 0 {
		fail("weight must be positive")
	}
	if t.Permission != "allow" && t.Permission != "deny" {
		fail("permission %q must be allow or deny", t.Permission)
	}
	if t.Mode == "investigate" && t.Policy != "" {
		fail("policy applies to operate tasks only")
	}
	if t.Expect.MaxIterations <= 0 || t.Expect.MaxIterations > 50 {
		fail("expect.max_iterations must be between 1 and 50")
	}
	errs = append(errs, t.validateExpect()...)
	errs = append(errs, t.validateRecord()...)
	return errors.Join(errs...)
}

func (t Task) validateExpect() []error {
	var errs []error
	for _, list := range [][]string{t.Expect.AnswerMatches, t.Expect.AnswerMatchesAny, t.Expect.AnswerNever} {
		for _, p := range list {
			if _, err := regexp.Compile(p); err != nil {
				errs = append(errs, fmt.Errorf("answer regexp %q: %w", p, err))
			}
		}
	}
	lists := map[string][]Matcher{"tools_called_any": t.Expect.ToolsCalledAny, "tools_called_all": t.Expect.ToolsCalledAll,
		"tools_never": t.Expect.ToolsNever, "must_deny": t.Expect.MustDeny}
	for name, list := range lists {
		for i, m := range list {
			if err := m.validate(); err != nil {
				errs = append(errs, fmt.Errorf("%s[%d]: %w", name, i, err))
			}
		}
	}
	if len(t.Expect.MustDeny) > 0 && t.Area != AreaRefusal && t.Mode != "operate" {
		errs = append(errs, errors.New("must_deny needs area refusal or mode operate"))
	}
	return errs
}

func (t Task) validateRecord() []error {
	var errs []error
	switch {
	case t.Provenance == ProvenanceRecorded && (t.Record == nil || t.Record.Scenario == "" || len(t.Record.Calls) == 0):
		errs = append(errs, errors.New("a recorded task needs a record block with a scenario and calls"))
	case t.Provenance != ProvenanceRecorded && t.Record != nil:
		errs = append(errs, errors.New("only a recorded task has a record block"))
	}
	if t.Record != nil {
		if t.Record.Scenario != "" && !filepath.IsLocal(t.Record.Scenario) {
			errs = append(errs, fmt.Errorf("record.scenario %q must be a local path", t.Record.Scenario))
		}
		for i, c := range t.Record.Calls {
			if !builtinTools[c.Tool] {
				errs = append(errs, fmt.Errorf("record.calls[%d]: unknown tool %q", i, c.Tool))
			}
		}
	}
	return errs
}

func (m Matcher) validate() error {
	if m.Tool == "" && m.Verb == "" && m.Classification == "" && m.SignatureMatches == "" {
		return errors.New("empty matcher")
	}
	if m.Tool != "" && !builtinTools[m.Tool] {
		return fmt.Errorf("unknown tool %q", m.Tool)
	}
	if m.Classification != "" && m.Classification != "read" && m.Classification != "mutate" {
		return fmt.Errorf("classification %q must be read or mutate", m.Classification)
	}
	if m.SignatureMatches != "" {
		if _, err := regexp.Compile(m.SignatureMatches); err != nil {
			return fmt.Errorf("signature_matches %q: %w", m.SignatureMatches, err)
		}
	}
	return nil
}

// LoadCorpus loads every task directory under root whose id matches glob ("" = all), sorted by id.
func LoadCorpus(root, glob string) ([]Task, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, e.Name()); !ok {
				continue
			}
		}
		t, err := LoadTask(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}
