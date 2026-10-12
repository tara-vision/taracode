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
// matches the directory it names, and the path is tried as written and with the symlinks of its
// existing part resolved, so a link reaches the protection of what it points to.
func matchPath(pattern, absPath, workingDir string) bool {
	pattern = filepath.ToSlash(pattern)
	var res []*regexp.Regexp
	if re, err := globRegexp(pattern, true, true); err == nil {
		res = append(res, re)
	}
	if dir, ok := strings.CutSuffix(pattern, "/**"); ok && dir != "" {
		if re, err := globRegexp(dir, true, true); err == nil {
			res = append(res, re)
		}
	}
	candidates := pathCandidates(pattern, absPath, workingDir)
	if real, ok := resolved(absPath); ok {
		wd := workingDir
		if realWd, ok := resolved(workingDir); ok && workingDir != "" {
			wd = realWd
		}
		candidates = append(candidates, pathCandidates(pattern, real, wd)...)
	}
	for _, re := range res {
		for _, c := range candidates {
			if re.MatchString(c) {
				return true
			}
		}
	}
	return false
}

// pathCandidates are the forms of a path a pattern is matched against: the absolute path, the path
// relative to workingDir when it lies under it, and the base name for a pattern without a slash.
func pathCandidates(pattern, absPath, workingDir string) []string {
	candidates := []string{filepath.ToSlash(absPath)}
	if workingDir != "" {
		if rel, err := filepath.Rel(workingDir, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			candidates = append(candidates, filepath.ToSlash(rel))
		}
	}
	if !strings.Contains(pattern, "/") {
		candidates = append(candidates, filepath.Base(absPath))
	}
	return candidates
}

// resolved returns the path with the symlinks of its longest existing part resolved, and whether that
// differs from the path. A path none of whose parts resolves is returned as it is.
func resolved(p string) (string, bool) {
	rest := ""
	for cur := p; ; {
		if real, err := evalSymlinks(cur); err == nil {
			out := filepath.Join(real, rest)
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
