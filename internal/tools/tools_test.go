package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "taracode-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return dir
}

func TestReadFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Create test file
	testFile := filepath.Join(dir, "test.txt")
	content := "line1\nline2\nline3\nline4\nline5"
	os.WriteFile(testFile, []byte(content), 0644)

	// Test full file read
	result, err := ReadFile(map[string]interface{}{"file_path": testFile}, dir)
	if err != nil {
		t.Errorf("ReadFile failed: %v", err)
	}
	if !strings.Contains(result, "line1") {
		t.Errorf("Expected content not found in result: %s", result)
	}

	// Test line range read
	result, err = ReadFile(map[string]interface{}{
		"file_path":  testFile,
		"start_line": float64(2),
		"end_line":   float64(4),
	}, dir)
	if err != nil {
		t.Errorf("ReadFile with range failed: %v", err)
	}
	if !strings.Contains(result, "line2") || !strings.Contains(result, "line4") {
		t.Errorf("Expected lines 2-4, got: %s", result)
	}

	// Test invalid range
	_, err = ReadFile(map[string]interface{}{
		"file_path":  testFile,
		"start_line": float64(10),
	}, dir)
	if err == nil {
		t.Error("Expected error for out of range start_line")
	}
}

func TestWriteFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	testFile := filepath.Join(dir, "new.txt")
	content := "hello world"

	result, err := WriteFile(map[string]interface{}{
		"file_path": testFile,
		"content":   content,
	}, dir)
	if err != nil {
		t.Errorf("WriteFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully") {
		t.Errorf("Unexpected result: %s", result)
	}

	// Verify content
	data, _ := os.ReadFile(testFile)
	if string(data) != content {
		t.Errorf("Content mismatch: expected %q, got %q", content, string(data))
	}

	// Test creating nested directories
	nestedFile := filepath.Join(dir, "nested", "deep", "file.txt")
	_, err = WriteFile(map[string]interface{}{
		"file_path": nestedFile,
		"content":   "nested content",
	}, dir)
	if err != nil {
		t.Errorf("WriteFile with nested dirs failed: %v", err)
	}
}

func TestAppendFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	testFile := filepath.Join(dir, "append.txt")
	os.WriteFile(testFile, []byte("original"), 0644)

	result, err := AppendFile(map[string]interface{}{
		"file_path": testFile,
		"content":   "\nappended",
	}, dir)
	if err != nil {
		t.Errorf("AppendFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully") {
		t.Errorf("Unexpected result: %s", result)
	}

	data, _ := os.ReadFile(testFile)
	if string(data) != "original\nappended" {
		t.Errorf("Content mismatch: got %q", string(data))
	}
}

func TestEditFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	testFile := filepath.Join(dir, "edit.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	// Test successful edit
	result, err := EditFile(map[string]interface{}{
		"file_path":  testFile,
		"old_string": "world",
		"new_string": "universe",
	}, dir)
	if err != nil {
		t.Errorf("EditFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully") {
		t.Errorf("Unexpected result: %s", result)
	}

	data, _ := os.ReadFile(testFile)
	if string(data) != "hello universe" {
		t.Errorf("Content mismatch: got %q", string(data))
	}

	// Test empty old_string
	_, err = EditFile(map[string]interface{}{
		"file_path":  testFile,
		"old_string": "",
		"new_string": "test",
	}, dir)
	if err == nil {
		t.Error("Expected error for empty old_string")
	}

	// Test string not found
	_, err = EditFile(map[string]interface{}{
		"file_path":  testFile,
		"old_string": "nonexistent",
		"new_string": "test",
	}, dir)
	if err == nil {
		t.Error("Expected error for string not found")
	}

	// Test replace_all
	os.WriteFile(testFile, []byte("foo bar foo baz foo"), 0644)
	result, err = EditFile(map[string]interface{}{
		"file_path":   testFile,
		"old_string":  "foo",
		"new_string":  "qux",
		"replace_all": true,
	}, dir)
	if err != nil {
		t.Errorf("EditFile replace_all failed: %v", err)
	}

	data, _ = os.ReadFile(testFile)
	if string(data) != "qux bar qux baz qux" {
		t.Errorf("Replace all failed: got %q", string(data))
	}
}

func TestInsertLines(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	testFile := filepath.Join(dir, "insert.txt")
	os.WriteFile(testFile, []byte("line1\nline2\nline3"), 0644)

	result, err := InsertLines(map[string]interface{}{
		"file_path":   testFile,
		"line_number": float64(2),
		"content":     "inserted",
	}, dir)
	if err != nil {
		t.Errorf("InsertLines failed: %v", err)
	}
	if !strings.Contains(result, "Successfully") {
		t.Errorf("Unexpected result: %s", result)
	}

	data, _ := os.ReadFile(testFile)
	expected := "line1\ninserted\nline2\nline3"
	if string(data) != expected {
		t.Errorf("Content mismatch: expected %q, got %q", expected, string(data))
	}

	// Test invalid line number
	_, err = InsertLines(map[string]interface{}{
		"file_path":   testFile,
		"line_number": float64(100),
		"content":     "test",
	}, dir)
	if err == nil {
		t.Error("Expected error for invalid line number")
	}
}

func TestReplaceLines(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	testFile := filepath.Join(dir, "replace.txt")
	os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4\nline5"), 0644)

	result, err := ReplaceLines(map[string]interface{}{
		"file_path":  testFile,
		"start_line": float64(2),
		"end_line":   float64(4),
		"content":    "replaced",
	}, dir)
	if err != nil {
		t.Errorf("ReplaceLines failed: %v", err)
	}
	if !strings.Contains(result, "Successfully") {
		t.Errorf("Unexpected result: %s", result)
	}

	data, _ := os.ReadFile(testFile)
	expected := "line1\nreplaced\nline5"
	if string(data) != expected {
		t.Errorf("Content mismatch: expected %q, got %q", expected, string(data))
	}
}

func TestDeleteLines(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	testFile := filepath.Join(dir, "delete.txt")
	os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4\nline5"), 0644)

	result, err := DeleteLines(map[string]interface{}{
		"file_path":  testFile,
		"start_line": float64(2),
		"end_line":   float64(4),
	}, dir)
	if err != nil {
		t.Errorf("DeleteLines failed: %v", err)
	}
	if !strings.Contains(result, "Successfully") {
		t.Errorf("Unexpected result: %s", result)
	}

	data, _ := os.ReadFile(testFile)
	expected := "line1\nline5"
	if string(data) != expected {
		t.Errorf("Content mismatch: expected %q, got %q", expected, string(data))
	}
}

func TestListFiles(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Create test structure
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "file2.go"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte(""), 0644)

	// Test non-recursive
	result, err := ListFiles(map[string]interface{}{
		"directory": dir,
		"recursive": false,
	}, dir)
	if err != nil {
		t.Errorf("ListFiles failed: %v", err)
	}
	if !strings.Contains(result, "file1.txt") || !strings.Contains(result, "subdir") {
		t.Errorf("Expected files not found: %s", result)
	}
	if strings.Contains(result, "nested.txt") {
		t.Error("Nested file should not appear in non-recursive listing")
	}

	// Test recursive
	result, err = ListFiles(map[string]interface{}{
		"directory": dir,
		"recursive": true,
	}, dir)
	if err != nil {
		t.Errorf("ListFiles recursive failed: %v", err)
	}
	if !strings.Contains(result, "nested.txt") {
		t.Errorf("Nested file should appear in recursive listing: %s", result)
	}
}

func TestFindFiles(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Create test structure
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "file2.go"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "nested.go"), []byte(""), 0644)

	result, err := FindFiles(map[string]interface{}{
		"pattern":   "*.go",
		"directory": dir,
	}, dir)
	if err != nil {
		t.Errorf("FindFiles failed: %v", err)
	}
	if !strings.Contains(result, "file2.go") || !strings.Contains(result, "nested.go") {
		t.Errorf("Expected .go files not found: %s", result)
	}
	if strings.Contains(result, "file1.txt") {
		t.Error("Non-matching file should not appear")
	}
}

func TestSearchFiles(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello world\nfoo bar\nhello again"), 0644)

	result, err := SearchFiles(map[string]interface{}{
		"pattern":   "hello",
		"directory": dir,
	}, dir)
	if err != nil {
		t.Errorf("SearchFiles failed: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("Search pattern not found in result: %s", result)
	}
}

func TestExecuteCommand(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	result, err := ExecuteCommand(map[string]interface{}{
		"command": "echo hello",
	}, dir)
	if err != nil {
		t.Errorf("ExecuteCommand failed: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("Expected output not found: %s", result)
	}
}

func TestGitStatus(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Initialize git repo
	ExecuteCommand(map[string]interface{}{"command": "git init"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.email 'test@test.com'"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.name 'Test'"}, dir)

	result, err := GitStatus(map[string]interface{}{}, dir)
	if err != nil {
		t.Errorf("GitStatus failed: %v", err)
	}
	// Empty repo should have clean status
	if !strings.Contains(result, "clean") && !strings.Contains(result, "No commits") {
		// Also accept empty output for fresh repo
		if result != "" && !strings.Contains(result, "Git Status") {
			t.Logf("GitStatus result: %s", result)
		}
	}

	// Create a file and check status again
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("test"), 0644)
	result, err = GitStatus(map[string]interface{}{}, dir)
	if err != nil {
		t.Errorf("GitStatus with changes failed: %v", err)
	}
}

func TestGitAdd(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Initialize git repo
	ExecuteCommand(map[string]interface{}{"command": "git init"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.email 'test@test.com'"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.name 'Test'"}, dir)

	// Create files
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("test1"), 0644)
	os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("test2"), 0644)

	result, err := GitAdd(map[string]interface{}{
		"files": []interface{}{"file1.txt", "file2.txt"},
	}, dir)
	if err != nil {
		t.Errorf("GitAdd failed: %v", err)
	}
	if !strings.Contains(result, "Staged") {
		t.Errorf("Unexpected result: %s", result)
	}
}

func TestGitCommit(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Initialize git repo
	ExecuteCommand(map[string]interface{}{"command": "git init"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.email 'test@test.com'"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.name 'Test'"}, dir)

	// Create and stage file
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("test"), 0644)
	GitAdd(map[string]interface{}{"files": []interface{}{"file.txt"}}, dir)

	result, err := GitCommit(map[string]interface{}{
		"message": "test commit",
	}, dir)
	if err != nil {
		t.Errorf("GitCommit failed: %v", err)
	}
	if !strings.Contains(result, "Commit") {
		t.Errorf("Unexpected result: %s", result)
	}
}

func TestGitLog(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Initialize git repo with a commit
	ExecuteCommand(map[string]interface{}{"command": "git init"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.email 'test@test.com'"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.name 'Test'"}, dir)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("test"), 0644)
	GitAdd(map[string]interface{}{"files": []interface{}{"file.txt"}}, dir)
	GitCommit(map[string]interface{}{"message": "initial commit"}, dir)

	result, err := GitLog(map[string]interface{}{"limit": float64(5)}, dir)
	if err != nil {
		t.Errorf("GitLog failed: %v", err)
	}
	if !strings.Contains(result, "initial commit") {
		t.Errorf("Commit message not found: %s", result)
	}
}

func TestGitBranch(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Initialize git repo with a commit
	ExecuteCommand(map[string]interface{}{"command": "git init"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.email 'test@test.com'"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.name 'Test'"}, dir)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("test"), 0644)
	GitAdd(map[string]interface{}{"files": []interface{}{"file.txt"}}, dir)
	GitCommit(map[string]interface{}{"message": "initial commit"}, dir)

	result, err := GitBranch(map[string]interface{}{}, dir)
	if err != nil {
		t.Errorf("GitBranch failed: %v", err)
	}
	// Should show main or master branch
	if !strings.Contains(result, "main") && !strings.Contains(result, "master") {
		t.Errorf("Expected branch not found: %s", result)
	}
}

func TestGitDiff(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Initialize git repo with a commit
	ExecuteCommand(map[string]interface{}{"command": "git init"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.email 'test@test.com'"}, dir)
	ExecuteCommand(map[string]interface{}{"command": "git config user.name 'Test'"}, dir)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("original"), 0644)
	GitAdd(map[string]interface{}{"files": []interface{}{"file.txt"}}, dir)
	GitCommit(map[string]interface{}{"message": "initial commit"}, dir)

	// Modify file
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0644)

	result, err := GitDiff(map[string]interface{}{}, dir)
	if err != nil {
		t.Errorf("GitDiff failed: %v", err)
	}
	if !strings.Contains(result, "modified") && !strings.Contains(result, "-original") {
		t.Logf("GitDiff result: %s", result)
	}
}

func TestCopyFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Create source file
	sourceFile := filepath.Join(dir, "source.txt")
	content := "source content"
	os.WriteFile(sourceFile, []byte(content), 0644)

	// Test successful copy
	destFile := filepath.Join(dir, "dest.txt")
	result, err := CopyFile(map[string]interface{}{
		"source_path": sourceFile,
		"dest_path":   destFile,
	}, dir)
	if err != nil {
		t.Errorf("CopyFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully copied") {
		t.Errorf("Unexpected result: %s", result)
	}

	// Verify destination exists and has same content
	data, _ := os.ReadFile(destFile)
	if string(data) != content {
		t.Errorf("Content mismatch: expected %q, got %q", content, string(data))
	}

	// Test copy with nested destination directories
	nestedDest := filepath.Join(dir, "nested", "deep", "copy.txt")
	_, err = CopyFile(map[string]interface{}{
		"source_path": sourceFile,
		"dest_path":   nestedDest,
	}, dir)
	if err != nil {
		t.Errorf("CopyFile with nested dirs failed: %v", err)
	}

	// Verify nested copy
	data, _ = os.ReadFile(nestedDest)
	if string(data) != content {
		t.Errorf("Nested copy content mismatch: got %q", string(data))
	}

	// Test error when source doesn't exist
	_, err = CopyFile(map[string]interface{}{
		"source_path": filepath.Join(dir, "nonexistent.txt"),
		"dest_path":   filepath.Join(dir, "dest2.txt"),
	}, dir)
	if err == nil {
		t.Error("Expected error when source doesn't exist")
	}
}

func TestMoveFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Create source file
	sourceFile := filepath.Join(dir, "source.txt")
	content := "move me"
	os.WriteFile(sourceFile, []byte(content), 0644)

	// Test successful move
	destFile := filepath.Join(dir, "dest.txt")
	result, err := MoveFile(map[string]interface{}{
		"source_path": sourceFile,
		"dest_path":   destFile,
	}, dir)
	if err != nil {
		t.Errorf("MoveFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully moved") {
		t.Errorf("Unexpected result: %s", result)
	}

	// Verify destination exists and source is gone
	data, _ := os.ReadFile(destFile)
	if string(data) != content {
		t.Errorf("Content mismatch: expected %q, got %q", content, string(data))
	}

	if _, err := os.Stat(sourceFile); err == nil {
		t.Error("Source file should not exist after move")
	}

	// Test move with nested destination directories
	sourceFile2 := filepath.Join(dir, "source2.txt")
	os.WriteFile(sourceFile2, []byte("nested move"), 0644)

	nestedDest := filepath.Join(dir, "nested", "deep", "moved.txt")
	_, err = MoveFile(map[string]interface{}{
		"source_path": sourceFile2,
		"dest_path":   nestedDest,
	}, dir)
	if err != nil {
		t.Errorf("MoveFile with nested dirs failed: %v", err)
	}

	// Verify nested move
	if _, err := os.Stat(nestedDest); err != nil {
		t.Error("Nested destination doesn't exist")
	}
	if _, err := os.Stat(sourceFile2); err == nil {
		t.Error("Source should not exist after nested move")
	}

	// Test error when source doesn't exist
	_, err = MoveFile(map[string]interface{}{
		"source_path": filepath.Join(dir, "nonexistent.txt"),
		"dest_path":   filepath.Join(dir, "dest2.txt"),
	}, dir)
	if err == nil {
		t.Error("Expected error when source doesn't exist")
	}
}

func TestDeleteFile(t *testing.T) {
	dir := setupTestDir(t)
	defer os.RemoveAll(dir)

	// Test delete single file
	testFile := filepath.Join(dir, "delete.txt")
	os.WriteFile(testFile, []byte("delete me"), 0644)

	result, err := DeleteFile(map[string]interface{}{
		"file_path": testFile,
	}, dir)
	if err != nil {
		t.Errorf("DeleteFile failed: %v", err)
	}
	if !strings.Contains(result, "Successfully deleted") {
		t.Errorf("Unexpected result: %s", result)
	}

	// Verify file is gone
	if _, err := os.Stat(testFile); err == nil {
		t.Error("File should not exist after delete")
	}

	// Test delete directory with recursive=true
	testDir := filepath.Join(dir, "deleteDir")
	os.MkdirAll(testDir, 0755)
	os.WriteFile(filepath.Join(testDir, "file.txt"), []byte("content"), 0644)

	result, err = DeleteFile(map[string]interface{}{
		"file_path": testDir,
		"recursive": true,
	}, dir)
	if err != nil {
		t.Errorf("DeleteFile recursive failed: %v", err)
	}

	if _, err := os.Stat(testDir); err == nil {
		t.Error("Directory should not exist after recursive delete")
	}

	// Test error when deleting directory without recursive=true
	testDir2 := filepath.Join(dir, "deleteDir2")
	os.MkdirAll(testDir2, 0755)

	_, err = DeleteFile(map[string]interface{}{
		"file_path": testDir2,
		"recursive": false,
	}, dir)
	if err == nil {
		t.Error("Expected error when deleting directory without recursive=true")
	}

	// Test succeed silently when file doesn't exist (idempotent)
	result, err = DeleteFile(map[string]interface{}{
		"file_path": filepath.Join(dir, "nonexistent.txt"),
	}, dir)
	if err != nil {
		t.Errorf("DeleteFile should succeed silently for nonexistent file: %v", err)
	}
	if !strings.Contains(result, "Successfully deleted") {
		t.Errorf("Expected success message for idempotent delete: %s", result)
	}
}
