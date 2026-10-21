package evals

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// The common store is a corpus-wide fixture store for the probes that do not depend on a task: which tool is
// installed on the sandbox and which version it is (spec 1b). It lives beside the tasks, in evals/common, with
// the calls that fill it in calls.yaml and the fixtures under fixtures/, and the replay consults it when a
// task's own store has no fixture for a signature. A task's fixture always wins.

// commonCallsFile is the list of calls that fill the common store: shell lines only, since the store answers
// tool-presence probes a model types into a shell.
type commonCallsFile struct {
	Calls []RecordCall `yaml:"calls"`
}

// LoadCommonCalls reads the calls of a common store's calls.yaml. Every call is a shell call with a command; a
// call of another tool, or with a key a record call does not take here, is refused with the file's name.
func LoadCommonCalls(path string) ([]RecordCall, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is the corpus's own calls file, a trusted path
	if err != nil {
		return nil, err
	}
	var f commonCallsFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, c := range f.Calls {
		if c.Tool != "shell" || str(c.Args, "command") == "" {
			return nil, fmt.Errorf("%s: call %d: the common store takes shell calls with a command only", path, i)
		}
		if c.DryRun {
			return nil, fmt.Errorf("%s: call %d: a common call is never a dry run", path, i)
		}
		if len(c.Env) > 0 {
			return nil, fmt.Errorf("%s: call %d: a common call carries no env", path, i)
		}
	}
	return f.Calls, nil
}

// RecordCommon records the calls of <dir>/calls.yaml into <dir>/fixtures with the recorder RecordTask uses
// (the same redaction and private-name refusal, the same swap that replaces the store only when every call
// succeeded), in a fresh temporary run directory and with no scenario: the calls ask the sandbox what it has.
func RecordCommon(ctx context.Context, dir string, resolve Resolver, out io.Writer) error {
	abs := dir
	if a, err := filepath.Abs(dir); err == nil {
		abs = a
	}
	calls, names, err := commonRecording(abs)
	if err != nil {
		return fmt.Errorf("common: %w", err)
	}
	runDir, err := os.MkdirTemp("", "taracode-record-common-*")
	if err != nil {
		return fmt.Errorf("common: %w", err)
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	tempParent, err := os.MkdirTemp(abs, "fixtures-recording-*")
	if err != nil {
		return fmt.Errorf("common: %w", err)
	}
	t := Task{ID: "common", Dir: abs, Record: &Record{Calls: calls}}
	return recordCalls(ctx, t, runDir, tempParent, resolve, names, out)
}

// commonRecording is what a common recording needs before it runs: the store's calls and the private names
// the recorder redacts.
func commonRecording(abs string) (calls []RecordCall, names []string, err error) {
	if calls, err = LoadCommonCalls(filepath.Join(abs, "calls.yaml")); err == nil {
		names, err = privateNames()
	}
	return calls, names, err
}
