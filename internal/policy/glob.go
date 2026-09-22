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

// matchPath reports whether a path glob matches the absolute path, the path relative to workingDir,
// or (for patterns without a slash) the base name.
func matchPath(pattern, absPath, workingDir string) bool {
	re, err := globRegexp(filepath.ToSlash(pattern), true, false)
	if err != nil {
		return false
	}
	candidates := []string{filepath.ToSlash(absPath)}
	if workingDir != "" {
		if rel, err := filepath.Rel(workingDir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			candidates = append(candidates, filepath.ToSlash(rel))
		}
	}
	if !strings.Contains(pattern, "/") {
		candidates = append(candidates, filepath.Base(absPath))
	}
	for _, c := range candidates {
		if re.MatchString(c) {
			return true
		}
	}
	return false
}

// collapse lowercases and squeezes whitespace so deny patterns match regardless of spacing.
func collapse(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(command), " "))
}
