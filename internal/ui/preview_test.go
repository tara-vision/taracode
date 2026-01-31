package ui

import (
	"strings"
	"testing"
)

func TestGenerateUnifiedDiff(t *testing.T) {
	tests := []struct {
		name        string
		oldContent  string
		newContent  string
		oldString   string
		newString   string
		wantContains []string
	}{
		{
			name:       "simple single line change",
			oldContent: "line1\nline2\nline3\nline4\nline5",
			newContent: "line1\nline2\nmodified\nline4\nline5",
			oldString:  "line3",
			newString:  "modified",
			wantContains: []string{
				"-line3",
				"+modified",
				"@@",
			},
		},
		{
			name:       "multi-line addition",
			oldContent: "func main() {\n    fmt.Println(\"hello\")\n}",
			newContent: "func main() {\n    fmt.Println(\"hello\")\n    fmt.Println(\"world\")\n}",
			oldString:  "    fmt.Println(\"hello\")\n}",
			newString:  "    fmt.Println(\"hello\")\n    fmt.Println(\"world\")\n}",
			wantContains: []string{
				"+",
				"world",
			},
		},
		{
			name:       "deletion",
			oldContent: "line1\nline2\nline3\nline4",
			newContent: "line1\nline4",
			oldString:  "line2\nline3\n",
			newString:  "",
			wantContains: []string{
				"-line2",
				"-line3",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GenerateUnifiedDiff(tc.oldContent, tc.newContent, tc.oldString, tc.newString)

			for _, want := range tc.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("GenerateUnifiedDiff() missing expected content %q in result:\n%s", want, result)
				}
			}
		})
	}
}

func TestEditPreviewStruct(t *testing.T) {
	preview := &EditPreview{
		FilePath:   "test.go",
		OldContent: "old content",
		NewContent: "new content",
		OldString:  "old",
		NewString:  "new",
	}

	if preview.FilePath != "test.go" {
		t.Error("FilePath not set correctly")
	}

	if preview.OldContent != "old content" {
		t.Error("OldContent not set correctly")
	}

	if preview.NewContent != "new content" {
		t.Error("NewContent not set correctly")
	}
}

func TestEditPreviewChoiceValues(t *testing.T) {
	// Verify the choice constants have distinct values
	if EditPreviewApply == EditPreviewCancel {
		t.Error("EditPreviewApply should not equal EditPreviewCancel")
	}

	if EditPreviewCancel == EditPreviewBackupThenApply {
		t.Error("EditPreviewCancel should not equal EditPreviewBackupThenApply")
	}

	if EditPreviewApply == EditPreviewBackupThenApply {
		t.Error("EditPreviewApply should not equal EditPreviewBackupThenApply")
	}
}
