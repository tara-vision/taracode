package policy

import (
	"path/filepath"
	"regexp"
	"strings"
)

// globRegexp turns a glob into an anchored regexp. In path mode * stops at a slash, ** crosses
// directories and a leading **/ also matches no directory at all; in plain mode * matches anything.
func globRegexp(pattern string, path, fold bool) (*regexp.Regexp, error) {
	var b strings.Builder
	if fold {
		b.WriteString("(?i)")
	}
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '*' && path && strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case c == '*' && path && strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*' && path:
			b.WriteString("[^/]*")
		case c == '*':
			b.WriteString(".*")
		case c == '?' && path:
			b.WriteString("[^/]")
		case c == '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// matchGlob reports whether a plain glob matches value, case-insensitively.
func matchGlob(pattern, value string) bool {
	re, err := globRegexp(pattern, false, true)
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

// evalSymlinks resolves the symlinks of a path; a variable so a test can take the file system away.
var evalSymlinks = filepath.EvalSymlinks

// matchPath reports whether a path glob matches the absolute path, the path relative to workingDir,
// or (for patterns without a slash) the base name, case-insensitively. A pattern ending in /** also
// matches the directory it names. The path and the working directory are each tried as written and
// with the symlinks of their existing part resolved, so a link reaches the protection of what it points to.
func matchPath(pattern, absPath, workingDir string) bool {
	m := newPathMatcher(pattern)
	realPath, _ := resolved(absPath)
	realWd, _ := resolved(workingDir)
	for _, path := range []string{absPath, realPath} {
		for _, wd := range []string{workingDir, realWd} {
			if m.matches(path, wd) {
				return true
			}
		}
	}
	return false
}

// pathMatcher is a path pattern compiled once: the pattern and, for one ending in /**, the directory it
// names. A walk matches many paths with one.
type pathMatcher struct {
	pattern string
	res     []*regexp.Regexp
}

func newPathMatcher(pattern string) pathMatcher {
	m := pathMatcher{pattern: filepath.ToSlash(pattern)}
	if re, err := globRegexp(m.pattern, true, true); err == nil {
		m.res = append(m.res, re)
	}
	if dir, ok := strings.CutSuffix(m.pattern, "/**"); ok && dir != "" {
		if re, err := globRegexp(dir, true, true); err == nil {
			m.res = append(m.res, re)
		}
	}
	return m
}

// matches reports whether the pattern matches a form of the path as written (pathCandidates).
func (m pathMatcher) matches(absPath, workingDir string) bool {
	for _, c := range pathCandidates(m.pattern, absPath, workingDir) {
		for _, re := range m.res {
			if re.MatchString(c) {
				return true
			}
		}
	}
	return false
}

// pathCandidates are the forms of a path a pattern is matched against: the absolute path, the path
// relative to workingDir when it lies under it in any letter case, and the base name for a pattern without a slash.
func pathCandidates(pattern, absPath, workingDir string) []string {
	candidates := []string{filepath.ToSlash(absPath)}
	if workingDir != "" {
		if rel, err := filepath.Rel(strings.ToLower(workingDir), strings.ToLower(absPath)); err == nil &&
			!strings.HasPrefix(rel, "..") {
			candidates = append(candidates, filepath.ToSlash(rel))
		}
	}
	if !strings.Contains(pattern, "/") {
		candidates = append(candidates, filepath.Base(absPath))
	}
	return candidates
}

// maxResolvedParts is the most path elements resolved walks back through, so a path of thousands of parts cannot
// stall the gate; the policy refuses a deeper target.
const maxResolvedParts = 256

// tooDeep reports whether a path has more parts than resolved walks back through.
func tooDeep(path string) bool {
	return strings.Count(path, string(filepath.Separator)) > maxResolvedParts
}

// resolved returns the path with the symlinks of its longest existing part resolved, and whether that
// differs from the path. An empty path, and a path none of whose parts resolves, is returned as it is.
func resolved(p string) (string, bool) {
	if p == "" || tooDeep(p) {
		return p, false
	}
	rest := ""
	for cur := p; ; {
		if target, err := evalSymlinks(cur); err == nil {
			out := filepath.Join(target, rest)
			return out, out != p
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, false
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// collapse lowercases and squeezes whitespace so deny patterns match regardless of spacing.
func collapse(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(command), " "))
}
