package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Permission is a remembered answer for a tool's mutate invocations.
type Permission string

// The three remembered answers a tool name can be set to.
const (
	Ask   Permission = "ask"
	Allow Permission = "allow"
	Deny  Permission = "deny"
)

// ParsePermission accepts the three names, case-insensitively.
func ParsePermission(s string) (Permission, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ask":
		return Ask, true
	case "allow":
		return Allow, true
	case "deny":
		return Deny, true
	}
	return "", false
}

const permissionsVersion = 3

type permissionsFile struct {
	Version int                   `json:"version"`
	Rules   map[string]Permission `json:"rules"`
}

// Permissions is the remembered answers, keyed by tool name; "*" applies to every tool without its
// own rule. Read invocations never consult it.
type Permissions struct {
	path  string
	mu    sync.RWMutex
	rules map[string]Permission
}

// LoadPermissions reads path; a missing file is an empty store. A file older than version 3 (the
// 2.x category format) is ignored and reported through ignored so the caller prints one notice.
func LoadPermissions(path string) (p *Permissions, ignored bool, err error) {
	p = &Permissions{path: path, rules: map[string]Permission{}}
	data, err := os.ReadFile(path) //nolint:gosec // the permissions file lives in the project's .taracode/
	if errors.Is(err, os.ErrNotExist) {
		return p, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var f permissionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	if f.Version != permissionsVersion {
		return p, true, nil
	}
	for tool, perm := range f.Rules {
		if parsed, ok := ParsePermission(string(perm)); ok {
			p.rules[tool] = parsed
		}
	}
	return p, false, nil
}

// AllowAll is an in-memory store that never asks; tests and non-interactive callers use it.
func AllowAll() *Permissions {
	return &Permissions{rules: map[string]Permission{"*": Allow}}
}

// For returns the rule for a tool: its own, else the "*" rule, else Ask.
func (p *Permissions) For(tool string) Permission {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if perm, ok := p.rules[tool]; ok {
		return perm
	}
	if perm, ok := p.rules["*"]; ok {
		return perm
	}
	return Ask
}

// Set records a rule and saves the file.
func (p *Permissions) Set(tool string, perm Permission) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules[tool] = perm
	return p.saveLocked()
}

// Reset forgets every rule and saves the file.
func (p *Permissions) Reset() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = map[string]Permission{}
	return p.saveLocked()
}

// Rules is a copy of the remembered rules.
func (p *Permissions) Rules() map[string]Permission {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]Permission, len(p.rules))
	for k, v := range p.rules {
		out[k] = v
	}
	return out
}

func (p *Permissions) saveLocked() error {
	if p.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(permissionsFile{Version: permissionsVersion, Rules: p.rules}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o644) //nolint:gosec // readable project settings, no secrets
}
