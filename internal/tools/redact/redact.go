// Package redact replaces secrets in tool output with [redacted:<kind>] before the text reaches the
// model, the session log, the history or the screen.
package redact

import (
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
)

// Options configures a Redactor.
type Options struct {
	ExtraPatterns []string // Go regular expressions; the whole match becomes [redacted:custom]
	Environ       []string // os.Environ() form; values of *KEY*, *TOKEN*, *SECRET*, *PASSWORD* variables are hidden
}

type pattern struct {
	kind string
	re   *regexp.Regexp
	keep bool // replace only the second capture group, keep the first
}

// Redactor applies the built-in patterns, the extra patterns and the environment values.
type Redactor struct {
	patterns []pattern
	env      []envValue
	count    atomic.Int64
}

type envValue struct {
	name, value string
}

// credentialValue matches "label: value" / "label=value" pairs for common credential-shaped labels
// (password, token, api key, and similar); only the value becomes [redacted:credential], the label
// and separator are kept. A trailing quote, if the value was quoted, is consumed and dropped.
var credentialValue = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|` +
	`access[_-]?key|auth[_-]?token|client[_-]?secret)\s*[=:]\s*["']?)([^\s"',;]{4,})["']?`)

var builtin = []pattern{
	{
		kind: "private-key",
		re:   regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	},
	{kind: "aws-access-key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{kind: "gcp-api-key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35,}\b`)},
	{kind: "github-token", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{kind: "slack-token", re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	{kind: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{kind: "azure-key", keep: true, re: regexp.MustCompile(`(AccountKey=)([A-Za-z0-9+/]{86,88}={0,2})`)},
	{kind: "azure-sas", keep: true, re: regexp.MustCompile(`([?&]sig=)([A-Za-z0-9%+/=]{20,})`)},
	{kind: "url-password", keep: true, re: regexp.MustCompile(`(://[^/\s:@]+:)([^@\s/]+)(@)`)},
	{
		kind: "credential",
		keep: true,
		re:   regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic)\s+)([A-Za-z0-9._~+/=-]{8,})`),
	},
	{kind: "credential", keep: true, re: credentialValue},
}

var sensitiveName = regexp.MustCompile(`(?i)KEY|TOKEN|SECRET|PASSWORD`)

// New compiles the extra patterns and collects the sensitive environment values (8 characters or
// longer; shorter values would redact ordinary text).
func New(opts Options) (*Redactor, error) {
	r := &Redactor{patterns: append([]pattern{}, builtin...)}
	for _, p := range opts.ExtraPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redact pattern %q: %w", p, err)
		}
		r.patterns = append(r.patterns, pattern{kind: "custom", re: re})
	}
	for _, kv := range opts.Environ {
		name, value, ok := strings.Cut(kv, "=")
		if ok && len(value) >= 8 && sensitiveName.MatchString(name) {
			r.env = append(r.env, envValue{name: name, value: value})
		}
	}
	return r, nil
}

// Redact returns s with every secret replaced and counts the spans it replaced.
func (r *Redactor) Redact(s string) string {
	for _, ev := range r.env {
		if n := strings.Count(s, ev.value); n > 0 {
			s = strings.ReplaceAll(s, ev.value, "[redacted:env:"+ev.name+"]")
			r.count.Add(int64(n))
		}
	}
	for _, p := range r.patterns {
		s = p.re.ReplaceAllStringFunc(s, func(match string) string {
			r.count.Add(1)
			if !p.keep {
				return "[redacted:" + p.kind + "]"
			}
			groups := p.re.FindStringSubmatch(match)
			suffix := ""
			if len(groups) > 3 {
				suffix = groups[3]
			}
			return groups[1] + "[redacted:" + p.kind + "]" + suffix
		})
	}
	return s
}

// Count is the number of spans redacted since the Redactor was created.
func (r *Redactor) Count() int64 { return r.count.Load() }
