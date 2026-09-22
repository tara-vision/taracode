package tools

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// argString returns a string argument, "" when absent or not a string.
func argString(args map[string]any, name string) string {
	s, _ := args[name].(string)
	return strings.TrimSpace(s)
}

// argInt returns an integer argument; JSON numbers arrive as float64, models sometimes send strings.
func argInt(args map[string]any, name string, def int) int {
	switch v := args[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// argBool returns a boolean argument; "true"/"false" strings are accepted.
func argBool(args map[string]any, name string) bool {
	switch v := args[name].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}

// required returns the named string argument or an error naming it.
func required(args map[string]any, name string) (string, error) {
	v := argString(args, name)
	if v == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return v, nil
}

// resolvePath makes a path absolute against workingDir and cleans it.
func resolvePath(path, workingDir string) string {
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingDir, path)
	}
	return filepath.Clean(path)
}
