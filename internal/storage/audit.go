package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// AuditRecord is one line of .taracode/audit.jsonl: a mutate-classified tool call and its decision,
// written before the tool runs.
type AuditRecord struct {
	Time           time.Time         `json:"time"`
	SessionID      string            `json:"session_id"`
	Mode           string            `json:"mode"`
	Tool           string            `json:"tool"`
	Verb           string            `json:"verb,omitempty"`
	Classification string            `json:"classification"`
	Command        string            `json:"command,omitempty"`
	Targets        map[string]string `json:"targets,omitempty"`
	Decision       string            `json:"decision"` // allow | deny
	// Rule names the policy layer that produced the decision: policy | mode | protected.<list> |
	// deny.commands | permission | user | dry_run.
	Rule   string `json:"rule"`
	Reason string `json:"reason,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// AuditPath is the log file.
func (m *Manager) AuditPath() string { return filepath.Join(m.rootDir, "audit.jsonl") }

// AppendAudit writes one record synchronously: append, then fsync, so nothing is lost at exit.
func (m *Manager) AppendAudit(rec AuditRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(m.AuditPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// ReadAudit returns the records of one session, or all of them when sessionID is "". Malformed
// lines are skipped.
func (m *Manager) ReadAudit(sessionID string) ([]AuditRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, err := os.Open(m.AuditPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []AuditRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var rec AuditRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if sessionID == "" || rec.SessionID == sessionID {
			out = append(out, rec)
		}
	}
	return out, sc.Err()
}

// ClearAudit deletes the log.
func (m *Manager) ClearAudit() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.Remove(m.AuditPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
