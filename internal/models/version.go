package models

import (
	"strconv"
	"strings"
)

// versionBefore reports whether have is an older Ollama release than need, comparing up to three
// numeric components (major.minor.patch). Either side being empty, or not parsing as numeric
// components, returns false: an unknown version is never reported as "before" a requirement, so
// callers stay silent rather than warn on a version they cannot understand.
func versionBefore(have, need string) bool {
	haveParts, ok := parseVersion(have)
	if !ok {
		return false
	}
	needParts, ok := parseVersion(need)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if haveParts[i] != needParts[i] {
			return haveParts[i] < needParts[i]
		}
	}
	return false
}

// parseVersion strips a leading "v" and anything from the first "-" or "+" onward (a pre-release
// or build suffix), then parses up to three dot-separated numeric components; missing trailing
// components are treated as 0. ok is false when v is empty, has more than three components, or
// any present component fails to parse as a non-negative integer.
func parseVersion(v string) (parts [3]int, ok bool) {
	if v == "" {
		return parts, false
	}
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	fields := strings.Split(v, ".")
	if len(fields) > 3 {
		return parts, false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}
