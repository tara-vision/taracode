package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tara-vision/taracode/internal/policy"
)

const (
	maxReadBytes     = 1 << 20 // read_file refuses larger files; use start_line/end_line
	maxSearchBytes   = 1 << 20 // search_files skips larger files
	defaultListMax   = 200
	defaultSearchMax = 100
)

// skippedDirs are never listed or searched.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".taracode": true, ".terraform": true,
	"__pycache__": true, ".venv": true, "dist": true, "target": true,
}

// FileTools returns read_file, list_files, search_files, write_file and edit_file.
func FileTools() []*Tool {
	return []*Tool{readFileTool(), listFilesTool(), searchFilesTool(), writeFileTool(), editFileTool()}
}

func readOnly(name string) func(map[string]any, string) policy.Invocation {
	return func(map[string]any, string) policy.Invocation {
		return policy.Invocation{Tool: name, Classification: policy.Read}
	}
}

func readFileTool() *Tool {
	return &Tool{
		Name: "read_file", ReadForm: true,
		Description: "Read a text file, optionally a line range. Paths are relative to the working directory.",
		Params: []Param{
			{Name: "path", Type: "string", Description: "File path", Required: true},
			{Name: "start_line", Type: "integer", Description: "First line to return (1-based)"},
			{Name: "end_line", Type: "integer", Description: "Last line to return (inclusive)"},
		},
		Classify: readOnly("read_file"),
		Run: func(_ context.Context, args map[string]any, workingDir string) (string, error) {
			path, err := required(args, "path")
			if err != nil {
				return "", err
			}
			abs := resolvePath(path, workingDir)
			info, err := os.Stat(abs)
			if err != nil {
				return "", err
			}
			if info.IsDir() {
				return "", fmt.Errorf("%s is a directory; use list_files", path)
			}
			if info.Size() > maxReadBytes && argInt(args, "end_line", 0) == 0 {
				return "", fmt.Errorf("%s is %d bytes; read it with start_line and end_line", path, info.Size())
			}
			data, err := os.ReadFile(abs) //nolint:gosec // the user asked for this path
			if err != nil {
				return "", err
			}
			return lineRange(string(data), argInt(args, "start_line", 0), argInt(args, "end_line", 0)), nil
		},
	}
}

// lineRange returns lines start..end (1-based, inclusive); 0 means from the first or to the last.
func lineRange(content string, start, end int) string {
	if start <= 0 && end <= 0 {
		return content
	}
	lines := strings.SplitAfter(content, "\n")
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "")
}

func listFilesTool() *Tool {
	return &Tool{
		Name: "list_files", ReadForm: true,
		Description: "List files and directories under a path, optionally recursive and filtered by a name glob.",
		Params: []Param{
			{Name: "path", Type: "string", Description: "Directory (default: working directory)"},
			{Name: "glob", Type: "string", Description: "Name pattern such as *.tf"},
			{Name: "recursive", Type: "boolean", Description: "Descend into subdirectories"},
			{Name: "max", Type: "integer", Description: "Maximum entries (default 200)"},
		},
		Classify: readOnly("list_files"),
		Run: func(_ context.Context, args map[string]any, workingDir string) (string, error) {
			root := resolvePath(argString(args, "path"), workingDir)
			glob, recursive, limit := argString(args, "glob"), argBool(args, "recursive"), argInt(args, "max", defaultListMax)
			var entries []string
			truncated := false
			err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if p == root {
					return nil
				}
				if d.IsDir() && skippedDirs[d.Name()] {
					return filepath.SkipDir
				}
				rel, _ := filepath.Rel(root, p)
				if glob == "" || matchName(glob, d.Name()) {
					if len(entries) >= limit {
						truncated = true
						return filepath.SkipAll
					}
					if d.IsDir() {
						rel += "/"
					}
					entries = append(entries, filepath.ToSlash(rel))
				}
				if d.IsDir() && !recursive {
					return filepath.SkipDir
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(entries) == 0 {
				return "No entries", nil
			}
			out := strings.Join(entries, "\n")
			if truncated {
				out += fmt.Sprintf("\n[%d entries shown; raise max or narrow the glob]", limit)
			}
			return out, nil
		},
	}
}

func matchName(glob, name string) bool {
	ok, err := filepath.Match(glob, name)
	return err == nil && ok
}

func searchFilesTool() *Tool {
	return &Tool{
		Name: "search_files", ReadForm: true,
		Description: "Search file contents with a regular expression (like grep -rn); returns path:line:text.",
		Params: []Param{
			{Name: "pattern", Type: "string", Description: "Go regular expression", Required: true},
			{Name: "path", Type: "string", Description: "Directory or file to search (default: working directory)"},
			{Name: "glob", Type: "string", Description: "Only files whose name matches, such as *.go"},
			{Name: "max", Type: "integer", Description: "Maximum matches (default 100)"},
		},
		Classify: readOnly("search_files"),
		Run: func(_ context.Context, args map[string]any, workingDir string) (string, error) {
			pattern, err := required(args, "pattern")
			if err != nil {
				return "", err
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("invalid pattern: %w", err)
			}
			root := resolvePath(argString(args, "path"), workingDir)
			glob, limit := argString(args, "glob"), argInt(args, "max", defaultSearchMax)
			var matches []string
			err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || len(matches) >= limit {
					return nil
				}
				if d.IsDir() {
					if p != root && skippedDirs[d.Name()] {
						return filepath.SkipDir
					}
					return nil
				}
				if glob != "" && !matchName(glob, d.Name()) {
					return nil
				}
				if info, err := d.Info(); err != nil || info.Size() > maxSearchBytes {
					return nil
				}
				rel, _ := filepath.Rel(workingDir, p)
				matches = append(matches, grepFile(p, filepath.ToSlash(rel), re, limit-len(matches))...)
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(matches) == 0 {
				return "No matches found", nil
			}
			out := strings.Join(matches, "\n")
			if len(matches) >= limit {
				out += fmt.Sprintf("\n[%d matches shown; narrow the pattern or raise max]", limit)
			}
			return out, nil
		},
	}
}

func grepFile(path, display string, re *regexp.Regexp, limit int) []string {
	f, err := os.Open(path) //nolint:gosec // walking the directory the user asked to search
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan() && len(out) < limit; n++ {
		line := sc.Text()
		if strings.IndexByte(line, 0) >= 0 {
			return out // binary
		}
		if re.MatchString(line) {
			if len(line) > 300 {
				line = line[:300] + "..."
			}
			out = append(out, fmt.Sprintf("%s:%d:%s", display, n, line))
		}
	}
	return out
}

func writeFileTool() *Tool {
	return &Tool{
		Name:        "write_file",
		Description: "Create or overwrite a file with the given content (directories are created).",
		Params: []Param{
			{Name: "path", Type: "string", Description: "File path", Required: true},
			{Name: "content", Type: "string", Description: "Complete file content", Required: true},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			return policy.Invocation{Tool: "write_file", Classification: policy.Mutate, Reason: "writes a file",
				Targets: policy.Targets{Paths: []string{resolvePath(argString(args, "path"), workingDir)}}}
		},
		Run: func(_ context.Context, args map[string]any, workingDir string) (string, error) {
			path, err := required(args, "path")
			if err != nil {
				return "", err
			}
			content, ok := args["content"].(string)
			if !ok {
				return "", fmt.Errorf("content is required")
			}
			abs := resolvePath(path, workingDir)
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil { //nolint:gosec // the user asked for this path
				return "", err
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil { //nolint:gosec // the user asked for this path
				return "", err
			}
			return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
		},
	}
}

func editFileTool() *Tool {
	return &Tool{
		Name:        "edit_file",
		Description: "Replace one exact occurrence of old with new in a file. preview=true only shows the diff.",
		Params: []Param{
			{Name: "path", Type: "string", Description: "File path", Required: true},
			{Name: "old", Type: "string", Description: "Exact text to replace (must occur once)", Required: true},
			{Name: "new", Type: "string", Description: "Replacement text", Required: true},
			{Name: "preview", Type: "boolean", Description: "Show the diff without writing"},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			if argBool(args, "preview") {
				return policy.Invocation{Tool: "edit_file", Classification: policy.Read}
			}
			return policy.Invocation{Tool: "edit_file", Classification: policy.Mutate, Reason: "edits a file",
				Targets: policy.Targets{Paths: []string{resolvePath(argString(args, "path"), workingDir)}}}
		},
		Run: func(_ context.Context, args map[string]any, workingDir string) (string, error) {
			path, err := required(args, "path")
			if err != nil {
				return "", err
			}
			oldText, ok := args["old"].(string)
			if !ok || oldText == "" {
				return "", fmt.Errorf("old is required")
			}
			newText, _ := args["new"].(string)
			abs := resolvePath(path, workingDir)
			data, err := os.ReadFile(abs) //nolint:gosec // the user asked for this path
			if err != nil {
				return "", err
			}
			content := string(data)
			switch n := strings.Count(content, oldText); {
			case n == 0:
				return "", fmt.Errorf("old text not found in %s", path)
			case n > 1:
				return "", fmt.Errorf("old text is ambiguous in %s (%d matches); include more context", path, n)
			}
			updated := strings.Replace(content, oldText, newText, 1)
			if argBool(args, "preview") {
				return unifiedDiff(path, content, updated), nil
			}
			if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil { //nolint:gosec // the user asked for this path
				return "", err
			}
			return fmt.Sprintf("Edited %s (%d -> %d bytes)", path, len(content), len(updated)), nil
		},
	}
}

// unifiedDiff renders a minimal unified diff of the changed region (context of 3 lines).
func unifiedDiff(path, before, after string) string {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	ctxStart := max(start-3, 0)
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", path, path,
		ctxStart+1, min(endA+3, len(a))-ctxStart, ctxStart+1, min(endB+3, len(b))-ctxStart)
	for i := ctxStart; i < start; i++ {
		sb.WriteString(" " + a[i] + "\n")
	}
	for i := start; i < endA; i++ {
		sb.WriteString("-" + a[i] + "\n")
	}
	for i := start; i < endB; i++ {
		sb.WriteString("+" + b[i] + "\n")
	}
	for i := endA; i < min(endA+3, len(a)); i++ {
		sb.WriteString(" " + a[i] + "\n")
	}
	return sb.String()
}
