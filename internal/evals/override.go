package evals

import (
	"fmt"
	"os"
	"regexp"
)

// overridePath is the one place a published override lives: docs/evals/overrides/<name>.registry.yaml, named as
// the repository holds it, so a results file points at a file a reader can open on GitHub.
var overridePath = regexp.MustCompile(`^docs/evals/overrides/[A-Za-z0-9][A-Za-z0-9._-]*\.registry\.yaml$`)

// CheckOverride checks the path a run or a results file names as the override its model ran from (spec 1h): a
// published file, docs/evals/overrides/<name>.registry.yaml, that exists where the command runs (the repository
// root).
func CheckOverride(path string) error {
	if !overridePath.MatchString(path) {
		return fmt.Errorf("the override must be a published file, docs/evals/overrides/<name>.registry.yaml, not %q", path)
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("the override %s is not a file in this checkout", path)
	}
	return nil
}

// LintResults checks the results files of a directory for what the board would publish wrongly: a file naming an
// override must name a published one (CheckOverride), and a live run (eval run --live) is a factory result that
// must not sit there at all. It returns the problems, one per line.
func LintResults(dir string) []string {
	all, err := ReadResults(dir)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, r := range all {
		if r.Live {
			problems = append(problems, fmt.Sprintf("%s %s: a live run (eval run --live) is a factory result, not a board row; "+
				"it must not sit in the results directory", r.Model, r.Date))
		}
		if r.Override == "" {
			continue
		}
		if err := CheckOverride(r.Override); err != nil {
			problems = append(problems, fmt.Sprintf("%s %s: %v", r.Model, r.Date, err))
		}
	}
	return problems
}
