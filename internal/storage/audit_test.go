package storage

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAuditAppendReadClear(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recs, err := m.ReadAudit("")
	if err != nil || len(recs) != 0 {
		t.Fatalf("empty log: %v %v", recs, err)
	}
	for i, sid := range []string{"s1", "s2", "s1"} {
		if err := m.AppendAudit(AuditRecord{Time: time.Now(), SessionID: sid, Mode: "operate", Tool: "shell", Classification: "mutate",
			Command: "make deploy", Decision: "allow", Rule: "policy", Targets: map[string]string{"n": string(rune('a' + i))}}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(m.AuditPath())
	if err != nil || strings.Count(string(data), "\n") != 3 {
		t.Fatalf("three lines expected: %q %v", data, err)
	}
	all, _ := m.ReadAudit("")
	s1, _ := m.ReadAudit("s1")
	if len(all) != 3 || len(s1) != 2 || s1[1].Targets["n"] != "c" {
		t.Fatalf("all %d s1 %+v", len(all), s1)
	}
	if err := os.WriteFile(m.AuditPath(), append(data, []byte("{broken\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := m.ReadAudit(""); err != nil || len(again) != 3 {
		t.Fatalf("malformed lines are skipped: %d %v", len(again), err)
	}
	if err := m.ClearAudit(); err != nil {
		t.Fatal(err)
	}
	if after, _ := m.ReadAudit(""); len(after) != 0 {
		t.Fatal("clear")
	}
	if err := m.ClearAudit(); err != nil {
		t.Fatal("clearing an absent log is fine")
	}
}
