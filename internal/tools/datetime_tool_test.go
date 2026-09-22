package tools

import (
	"context"
	"strings"
	"testing"
)

func TestDateTimeFormatsAndZones(t *testing.T) {
	tool := DateTimeTool()
	out, err := tool.Run(context.Background(), map[string]any{}, "")
	if err != nil || !strings.Contains(out, "T") {
		t.Fatalf("%q %v", out, err)
	}
	out, err = tool.Run(context.Background(), map[string]any{"format": "unix"}, "")
	if err != nil || len(out) < 10 {
		t.Fatalf("%q %v", out, err)
	}
	out, err = tool.Run(context.Background(), map[string]any{"timezone": "Europe/Skopje"}, "")
	if err != nil || !strings.Contains(out, "Europe/Skopje") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"timezone": "Mars/Olympus"}, ""); err == nil {
		t.Error("unknown zone must error")
	}
}
