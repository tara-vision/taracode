package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Results is one model's run over the corpus (spec 9).
type Results struct {
	Taracode    string       `json:"taracode"`
	Ollama      string       `json:"ollama"`
	Model       string       `json:"model"`
	Tier        string       `json:"tier"`
	Think       string       `json:"think"`
	Temperature float64      `json:"temperature"`
	Date        string       `json:"date"`
	Runs        int          `json:"runs"`
	Host        string       `json:"host"`
	Tasks       []TaskResult `json:"tasks"`
	Summary     Summary      `json:"summary"`
}

// TaskResult is one task's row. The scores and the counts are per run: with --runs N each is the
// mean over the runs (ruling P3-R48), the counts rounded to whole numbers for display only. The
// summary's rates, the fixture miss rate and the mean iterations, come from the unrounded per-run
// values instead, pooled over the runs (ruling P3-R56). Error and Notes never carry an engine
// address or a host path (ruling P3-R45).
type TaskResult struct {
	ID               string   `json:"id"`
	Area             string   `json:"area"`
	Score            float64  `json:"score"`             // per-run mean with --runs
	Pass             bool     `json:"pass"`              // the mean score against the pass mark
	Tools            float64  `json:"tools"`             // per-run mean with --runs
	Answer           float64  `json:"answer"`            // per-run mean with --runs
	Forbidden        float64  `json:"forbidden"`         // per-run mean with --runs
	Iterations       int      `json:"iterations"`        // per-run mean with --runs, rounded
	ToolCalls        int      `json:"tool_calls"`        // per-run mean with --runs, rounded
	Denied           int      `json:"denied"`            // per-run mean with --runs, rounded
	FixtureMisses    int      `json:"fixture_misses"`    // per-run mean with --runs, rounded
	PromptTokens     int      `json:"prompt_tokens"`     // per-run mean with --runs, rounded
	CompletionTokens int      `json:"completion_tokens"` // per-run mean with --runs, rounded
	WallMs           int64    `json:"wall_ms"`           // per-run mean with --runs, rounded
	Truncated        bool     `json:"truncated"`         // any run hit the iteration cap
	TimedOut         bool     `json:"timed_out"`         // any run hit the task limit
	SafetyFailure    bool     `json:"safety_failure"`    // any run's gate allowed a must_deny call
	Error            string   `json:"error"`             // the first error of the runs, scrubbed
	Notes            []string `json:"notes,omitempty"`   // the scorer's notes, scrubbed

	means *runMeans // the unrounded per-run counts of a row folded from several runs; not serialized
}

// runMeans are the unrounded per-run means of the counts the summary derives its rates from (ruling
// P3-R56). averageRuns keeps them on a row folded from several runs, whose own counts are rounded
// for display. A single run's row has none, and neither has a row read back from a results file:
// the summary is computed once, by Run, and stored.
type runMeans struct {
	iterations, toolCalls, fixtureMisses float64
}

// perRun is the row's unrounded per-run iterations, tool calls and fixture misses: the means
// averageRuns kept, else the row's own counts.
func (r TaskResult) perRun() (iterations, toolCalls, fixtureMisses float64) {
	if r.means != nil {
		return r.means.iterations, r.means.toolCalls, r.means.fixtureMisses
	}
	return float64(r.Iterations), float64(r.ToolCalls), float64(r.FixtureMisses)
}

// AreaSummary is the per-area breakdown.
type AreaSummary struct {
	Tasks     int     `json:"tasks"`
	Passed    int     `json:"passed"`
	MeanScore float64 `json:"mean_score"`
}

// Summary is the run's totals.
type Summary struct {
	PassRate        float64                `json:"pass_rate"`
	MeanScore       float64                `json:"mean_score"`
	MeanIterations  float64                `json:"mean_iterations"`
	MeanWallMs      float64                `json:"mean_wall_ms"`
	FixtureMissRate float64                `json:"fixture_miss_rate"`
	SafetyFailures  int                    `json:"safety_failures"`
	ByArea          map[string]AreaSummary `json:"by_area"`
}

// modelSlug makes a model name safe for a file name.
func modelSlug(model string) string {
	return strings.ToLower(strings.NewReplacer(":", "-", "/", "-").Replace(model))
}

// WriteResults writes <dir>/<model slug>-<date>.json and returns its path.
func WriteResults(dir string, r Results) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // docs/evals/results is repository content
		return "", err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, modelSlug(r.Model)+"-"+r.Date+".json")
	return path, os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // repository content
}

// ReadResults reads every *.json under dir, sorted by model and then date.
func ReadResults(dir string) ([]Results, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Results
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // a results file the caller points at
		if err != nil {
			return nil, err
		}
		var r Results
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model+out[i].Date < out[j].Model+out[j].Date })
	return out, nil
}

// summarize computes the totals of a run from its task results and the tasks' weights. The mean
// iterations and the fixture miss rate come from the rows' unrounded per-run values (perRun, ruling
// P3-R56). Every task of a run has the same number of runs, so the per-run means pool exactly: the
// miss rate is the misses of every run over the calls of every run.
func summarize(tasks []Task, results []TaskResult) Summary {
	s := Summary{ByArea: map[string]AreaSummary{}}
	if len(results) == 0 {
		return s
	}
	weights := map[string]float64{}
	for _, t := range tasks {
		weights[t.ID] = t.Weight
	}
	var passed int
	var weightSum, scoreSum, iterations, wall, calls, misses float64
	areaScores := map[string][]float64{}
	for _, r := range results {
		w := weights[r.ID]
		if w == 0 {
			w = 1
		}
		weightSum += w
		scoreSum += w * r.Score
		runIterations, runCalls, runMisses := r.perRun()
		iterations += runIterations
		wall += float64(r.WallMs)
		calls += runCalls
		misses += runMisses
		if r.Pass {
			passed++
		}
		if r.SafetyFailure {
			s.SafetyFailures++
		}
		a := s.ByArea[r.Area]
		a.Tasks++
		if r.Pass {
			a.Passed++
		}
		s.ByArea[r.Area] = a
		areaScores[r.Area] = append(areaScores[r.Area], r.Score)
	}
	n := float64(len(results))
	s.PassRate = round3(float64(passed) / n)
	s.MeanScore = round3(scoreSum / weightSum)
	s.MeanIterations = round3(iterations / n)
	s.MeanWallMs = round3(wall / n)
	if calls > 0 {
		s.FixtureMissRate = round3(misses / calls)
	}
	for area, scores := range areaScores {
		sum := 0.0
		for _, v := range scores {
			sum += v
		}
		a := s.ByArea[area]
		a.MeanScore = round3(sum / float64(len(scores)))
		s.ByArea[area] = a
	}
	return s
}

// publicError is err as a results file may carry it (ruling P3-R45): without the engine's address or
// a host path. A URL error loses its operation and URL, and a failed connection, a name that did not
// resolve and an expired deadline become fixed phrases; publicText then scrubs what is left. The raw
// text stays on the terminal and in the transcript.
func publicError(err error, opts RunOptions, t Task) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		text = strings.Replace(text, urlErr.Error(), publicCause(urlErr.Err), 1)
	} else if phrase, matched := enginePhrase(err); phrase != "" {
		text = strings.Replace(text, matched, phrase, 1)
	}
	return publicText(text, opts, t)
}

// publicCause is the fixed phrase for an engine-side cause, else the cause's own text.
func publicCause(err error) string {
	if phrase, _ := enginePhrase(err); phrase != "" {
		return phrase
	}
	return err.Error()
}

// enginePhrase maps an expired deadline, a name that did not resolve and a failed connection to a
// fixed phrase, and returns the text of the error it matched as well.
func enginePhrase(err error) (phrase, matched string) {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the turn timed out", context.DeadlineExceeded.Error()
	case errors.As(err, &dnsErr):
		return "the engine name did not resolve", dnsErr.Error()
	case errors.As(err, &opErr):
		return "the engine connection failed", opErr.Error()
	}
	return "", ""
}

// publicNotes is publicText over the scorer's notes.
func publicNotes(notes []string, opts RunOptions, t Task) []string {
	if notes == nil {
		return nil
	}
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = publicText(n, opts, t)
	}
	return out
}

// publicText scrubs a text for a results file (ruling P3-R45): the task directory becomes
// evals/tasks/<id>, the temporary directory <tmp>, the home directory ~ (and so does any user home a
// path starts with, the engine's own included, ruling P3-R56), the engine's URL, address and host
// name <engine>, and any other IP address, with or without a port, <addr>.
func publicText(s string, opts RunOptions, t Task) string {
	for _, p := range scrubbedPaths(opts, t) {
		s = replaceBounded(s, p[0], p[1], isPathByte, isPathByte)
	}
	s = scrubUserHomes(s)
	for _, form := range engineForms(opts.Host) {
		s = strings.ReplaceAll(s, form, "<engine>")
	}
	s = ipLiteral.ReplaceAllString(s, "<addr>")
	if host := engineHostname(opts.Host); host != "" {
		s = replaceBounded(s, host, "<engine>", isHostByte, isHostByte)
	}
	return s
}

// ipLiteral matches an IPv6 address in brackets, an IPv4 address and a bare IPv6 address (the full
// form, or one with "::" and a group on at least one side), each with an optional port. A bare IPv6
// address may end in a dotted quad, as an IPv4-mapped one does (::ffff:192.0.2.10, ruling P3-R56).
var ipLiteral = regexp.MustCompile(`\[[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*(?:%[0-9A-Za-z._-]+)?\](?::\d{1,5})?` +
	`|\b\d{1,3}(?:\.\d{1,3}){3}(?::\d{1,5})?\b` +
	`|\b(?:[0-9A-Fa-f]{1,4}:){6}\d{1,3}(?:\.\d{1,3}){3}\b` +
	`|\b(?:[0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}\b` +
	`|\b[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6}::(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6})?` + dottedQuadTail +
	`|::[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6}` + dottedQuadTail + `\b`)

// dottedQuadTail continues an IPv6 address whose last group starts an embedded IPv4 address.
const dottedQuadTail = `(?:(?:\.\d{1,3}){3})?`

// userHome matches the start of a user's home directory: /home/<user>, /Users/<user> or /root.
var userHome = regexp.MustCompile(`/home/[A-Za-z0-9._-]+|/Users/[A-Za-z0-9._-]+|/root`)

// scrubUserHomes rewrites a user's home directory to ~ wherever it starts a path, whoever the user
// is: a status body from the engine can name the engine's own home, such as an .ollama directory
// (ruling P3-R56). A match inside a longer path (/var/home/x) or a longer name (/rootfs) stays.
func scrubUserHomes(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range userHome.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && (isPathByte(s[start-1]) || s[start-1] == '/') || end < len(s) && isPathByte(s[end]) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString("~")
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// scrubbedPaths are the host paths publicText replaces, each also in its symlink-free form, longest
// first so a task directory under the home directory keeps its own replacement.
func scrubbedPaths(opts RunOptions, t Task) [][2]string {
	home := ""
	if opts.scope != nil {
		home = opts.scope.home // the real home: HOME is isolated while Run runs
	} else {
		home, _ = os.UserHomeDir()
	}
	var pairs [][2]string
	add := func(path, with string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if path == "/" || path == "." {
			return
		}
		pairs = append(pairs, [2]string{path, with})
		if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
			pairs = append(pairs, [2]string{resolved, with})
		}
	}
	if t.Dir != "" {
		if abs, err := filepath.Abs(t.Dir); err == nil {
			add(abs, "evals/tasks/"+t.ID)
		}
	}
	add(os.TempDir(), "<tmp>")
	add(home, "~")
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i][0]) > len(pairs[j][0]) })
	return pairs
}

// engineForms are the engine's URL as given and as scheme://host[:port], and its host[:port],
// longest first.
func engineForms(host string) []string {
	host = strings.TrimRight(strings.TrimSpace(host), "/") // http://h:11434// is http://h:11434
	if host == "" {
		return nil
	}
	forms := []string{host}
	if u := engineURL(host); u != nil {
		forms = append(forms, u.Scheme+"://"+u.Host, u.Host)
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// engineHostname is the engine's bare host name, "" when the host does not parse.
func engineHostname(host string) string {
	if u := engineURL(strings.TrimSpace(host)); u != nil {
		return u.Hostname()
	}
	return ""
}

// engineURL parses the engine's host, reading a bare host[:port] as http.
func engineURL(host string) *url.URL {
	if host == "" {
		return nil
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

// replaceBounded replaces every occurrence of old whose neighbours are not part of a longer name:
// before(c) and after(c) report the bytes that would continue it on either side.
func replaceBounded(s, old, with string, before, after func(byte) bool) string {
	var b strings.Builder
	for {
		i := strings.Index(s, old)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(old)
		if (i > 0 && before(s[i-1])) || (end < len(s) && after(s[end])) {
			b.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(with)
		s = s[end:]
	}
}

// isPathByte reports a byte that continues a file name.
func isPathByte(c byte) bool {
	return c == '.' || c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isHostByte reports a byte that continues a host name.
func isHostByte(c byte) bool {
	return c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
