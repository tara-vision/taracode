package policy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// dirWalkBound is the most entries the walk of a directory target reads before it refuses; a variable so a
// test can lower it.
var dirWalkBound = 50000

// skippedTrees are the directories package managers and terraform's backend cache own. No tool keeps state the
// policy protects in them (.terraform holds a cache of the backend that terraform init rebuilds; the state sits
// beside it), so the walk of a directory target does not descend into them, and a target that is one of them is not
// walked at all.
var skippedTrees = map[string]bool{"node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, ".gradle": true, ".terraform": true}

// errWalkBound stops a walk that read dirWalkBound entries.
var errWalkBound = errors.New("the walk reached its bound")

// heldPath reports a protected path that a directory target of a shell command holds, since removing,
// moving or writing into the directory reaches it. A pattern that names one fixed path or the tree under
// one (.git/**, infra/prod/**, an absolute file) is checked by looking for that path under the directory;
// the other patterns by a walk of at most dirWalkBound entries, which refuses when it reaches the bound.
func (p Policy) heldPath(dir, workingDir string) (Verdict, bool) {
	realWd, _ := resolved(workingDir)
	var walked []pathMatcher
	for _, pat := range p.Protected.Paths {
		fixed, ok := literalPart(pat)
		if !ok {
			walked = append(walked, newPathMatcher(pat))
			continue
		}
		if !filepath.IsAbs(fixed) {
			fixed = filepath.Join(realWd, fixed)
		}
		if fixed, _ = resolved(fixed); within(fixed, dir) && exists(fixed) {
			return denied("protected.paths", "%s holds %s, which the protected path pattern %q covers", dir, fixed,
				pat), true
		}
	}
	if len(walked) == 0 || skippedTrees[filepath.Base(dir)] {
		return Verdict{}, false
	}
	return walkFor(dir, realWd, walked)
}

// walkFor walks a directory, skipping the trees package managers own, for an entry a pattern matches.
func walkFor(dir, workingDir string, walked []pathMatcher) (Verdict, bool) {
	var v Verdict
	var found bool
	entries := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil // the walk checks what it can read; an unreadable entry is left out
		}
		if entries++; entries > dirWalkBound {
			return errWalkBound
		}
		for _, m := range walked {
			if m.matches(path, workingDir) {
				v, found = denied("protected.paths", "%s holds %s, which matches the protected path pattern %q",
					dir, path, m.pattern), true
				return filepath.SkipAll
			}
		}
		if d.IsDir() && skippedTrees[d.Name()] {
			return filepath.SkipDir
		}
		return nil
	})
	if errors.Is(err, errWalkBound) {
		return denied("protected.paths", "%s holds more than %d entries, too many to check for protected paths; "+
			"remove it yourself or name a smaller path", dir, dirWalkBound), true
	}
	return v, found
}

// literalPart returns the fixed path a pattern names when it names one path or the whole tree under one,
// and false for a pattern that a walk has to check (one with a glob, or without a slash, which matches a
// base name anywhere).
func literalPart(pattern string) (string, bool) {
	pattern = filepath.ToSlash(pattern)
	fixed := strings.TrimSuffix(pattern, "/**")
	if fixed == "" || !strings.Contains(pattern, "/") || strings.ContainsAny(fixed, "*?[") {
		return "", false
	}
	return filepath.FromSlash(fixed), true
}

// within reports whether path is dir or lies under it, in any letter case.
func within(path, dir string) bool {
	rel, err := filepath.Rel(strings.ToLower(dir), strings.ToLower(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// exists reports whether something is at path, without following a final symlink.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// dirTarget returns the directory a shell target names, through a link to it: rm -rf link/ removes what the link
// points to, and the slash is gone by the time the policy sees the path.
func dirTarget(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	target, _ := resolved(path)
	return target, true
}
