package policy

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// Find is what a find narrowed by name deletes or runs a command on: the entries under its start point that one
// of its name tests matches.
type Find struct {
	Start   string     // the start point, absolute
	Written string     // the start point as the command writes it, which begins every path find prints
	Tests   []NameTest // the name tests of the find's expression
}

// NameTest is one name test of a find: -name and -iname match an entry's base name; -path, -ipath, -wholename
// and -iwholename the path find prints; -regex and -iregex that path as a regular expression.
type NameTest struct {
	Test, Pattern string
}

// foundPath reports a protected path among the entries a find acts on: under its start point (walkUnder), an
// entry one of its name tests matches, or anything under a directory one matches, which -exec rm -rf {} takes
// with it, that a protected path pattern covers.
func (p Policy) foundPath(f Find, workingDir string) (Verdict, bool) {
	dir, ok := dirTarget(f.Start)
	if !ok {
		return Verdict{}, false
	}
	realWd, _ := resolved(workingDir)
	protected := make([]pathMatcher, 0, len(p.Protected.Paths))
	for _, pat := range p.Protected.Paths {
		protected = append(protected, newPathMatcher(pat))
	}
	tests := make([]findTest, 0, len(f.Tests))
	for _, t := range f.Tests {
		tests = append(tests, newFindTest(t))
	}
	matched := "" // the directory a test matched that the walk is inside
	return walkUnder(dir, func(path string, d fs.DirEntry) (Verdict, bool) {
		if matched != "" && !within(path, matched) {
			matched = ""
		}
		rel, _ := filepath.Rel(dir, path)
		printed := strings.TrimSuffix(f.Written, "/") + "/" + filepath.ToSlash(rel)
		if matched == "" && !anyFindTest(tests, d.Name(), printed) {
			return Verdict{}, false
		}
		if matched == "" && d.IsDir() {
			matched = path
		}
		for _, m := range protected {
			if m.matches(path, realWd) {
				return denied("protected.paths", "the find from %s acts on %s, which matches the protected path pattern %q",
					f.Written, path, m.pattern), true
			}
		}
		return Verdict{}, false
	})
}

// findTest is one name test compiled the way find matches it, case-insensitively: by the base name, or by the
// printed path with a * that crosses slashes. A nil pattern matches every entry.
type findTest struct {
	re     *regexp.Regexp
	byPath bool
}

// newFindTest compiles a name test. A glob with a bracket or a backslash, and a regular expression that Go does
// not read as find does (an emacs group or alternative) or cannot compile, match every entry: the check then
// errs toward refusing.
func newFindTest(t NameTest) findTest {
	switch t.Test {
	case "-name", "-iname":
		return findTest{re: findGlob(t.Pattern)}
	case "-regex", "-iregex":
		if strings.Contains(t.Pattern, `\(`) || strings.Contains(t.Pattern, `\|`) {
			return findTest{byPath: true}
		}
		re, _ := regexp.Compile("(?i)^(?:" + t.Pattern + ")$")
		return findTest{re: re, byPath: true}
	}
	return findTest{re: findGlob(t.Pattern), byPath: true}
}

func findGlob(pattern string) *regexp.Regexp {
	if strings.ContainsAny(pattern, `[\`) {
		return nil
	}
	re, _ := globRegexp(pattern, false, true)
	return re
}

func anyFindTest(tests []findTest, base, printed string) bool {
	for _, t := range tests {
		subject := base
		if t.byPath {
			subject = printed
		}
		if t.re == nil || t.re.MatchString(subject) {
			return true
		}
	}
	return false
}
