package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

// ErrNoDryRun says the tool has no dry run for this invocation.
var ErrNoDryRun = errors.New("this tool has no dry run")

// Options configures a registry.
type Options struct {
	Offline  bool             // hide tools that talk to the internet
	Redactor *redact.Redactor // nil = no redaction
	History  *history.Manager // nil = no backups and no operation history
}

// Registry holds the tools in registration order.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]*Tool
	order    []string
	mcp      map[string]string // tool name -> server
	offline  bool
	redactor *redact.Redactor
	history  *history.Manager
}

// NewRegistry returns an empty registry.
func NewRegistry(opts Options) *Registry {
	return &Registry{
		tools: map[string]*Tool{}, mcp: map[string]string{},
		offline: opts.Offline, redactor: opts.Redactor, history: opts.History,
	}
}

// Register adds a built-in tool. A duplicate name is a programming error.
func (r *Registry) Register(t *Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name]; exists {
		panic("tools: duplicate tool " + t.Name)
	}
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
}

// RegisterMCP adds a tool discovered on an MCP server; re-registering a name replaces it.
func (r *Registry) RegisterMCP(t *Tool, server string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name]; !exists {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = t
	r.mcp[t.Name] = server
}

// UnregisterMCP removes every tool of a server.
func (r *Registry) UnregisterMCP(server string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.order[:0]
	for _, name := range r.order {
		if r.mcp[name] == server {
			delete(r.tools, name)
			delete(r.mcp, name)
			continue
		}
		kept = append(kept, name)
	}
	r.order = kept
}

// MCPTools lists the MCP tool names by server.
func (r *Registry) MCPTools() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string][]string{}
	for _, name := range r.order {
		if server, ok := r.mcp[name]; ok {
			out[server] = append(out[server], name)
		}
	}
	return out
}

// IsMCP reports whether a tool came from an MCP server.
func (r *Registry) IsMCP(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.mcp[name]
	return ok
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (*Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names lists every tool in registration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string{}, r.order...)
}

// SetHistory wires the operation history for write_file and edit_file backups and records.
func (r *Registry) SetHistory(h *history.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = h
}

// Redactions is the number of secret spans redacted so far.
func (r *Registry) Redactions() int64 {
	if r.redactor == nil {
		return 0
	}
	return r.redactor.Count()
}

func (r *Registry) exposed(t *Tool, mode policy.Mode) bool {
	if r.offline && t.External {
		return false
	}
	return mode == policy.ModeOperate || t.ReadForm
}

// Definitions renders the schemas of the tools exposed in mode, in registration order.
//
//nolint:revive // openaiTool aliases the exported openai.Tool, not a distinct unexported type
func (r *Registry) Definitions(mode policy.Mode) []openaiTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var defs []openaiTool
	for _, name := range r.order {
		if t := r.tools[name]; r.exposed(t, mode) {
			defs = append(defs, t.definition())
		}
	}
	return defs
}

// Available counts the tools exposed in mode.
func (r *Registry) Available(mode policy.Mode) int { return len(r.Definitions(mode)) }

// Classify runs the tool's classifier and fills in the working directory.
func (r *Registry) Classify(name string, args map[string]any, workingDir string) (policy.Invocation, error) {
	t, ok := r.Get(name)
	if !ok {
		return policy.Invocation{}, fmt.Errorf("unknown tool %q", name)
	}
	inv := t.Classify(args, workingDir)
	inv.Tool = name
	inv.WorkingDir = workingDir
	return inv, nil
}

// Execute runs a tool: backup before a file mutation, redaction of the output, and a history record.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any, workingDir string) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	target, backup := r.backupBefore(name, args, workingDir)
	start := time.Now()
	out, err := t.Run(ctx, args, workingDir)
	out = r.redact(out)
	if err != nil {
		err = errors.New(r.redact(err.Error()))
		return out, err
	}
	r.recordAfter(name, args, target, backup, err, start)
	return out, err
}

// DryRun runs the tool's dry run when it has one.
func (r *Registry) DryRun(ctx context.Context, name string, args map[string]any, workingDir string) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if t.DryRun == nil {
		return "", ErrNoDryRun
	}
	out, err := t.DryRun(ctx, args, workingDir)
	return r.redact(out), err
}

func (r *Registry) redact(s string) string {
	if r.redactor == nil {
		return s
	}
	return r.redactor.Redact(s)
}

// backupBefore backs the target of write_file and edit_file up when the history is on and the file
// exists; it returns the target path and the backup path.
func (r *Registry) backupBefore(name string, args map[string]any, workingDir string) (target, backup string) {
	if r.history == nil || (name != "write_file" && name != "edit_file") || argBool(args, "preview") {
		return "", ""
	}
	target = resolvePath(argString(args, "path"), workingDir)
	if _, err := os.Stat(target); err != nil {
		return target, ""
	}
	backup, _ = r.history.CreateBackup(target)
	return target, backup
}

func (r *Registry) recordAfter(name string, args map[string]any, target, backup string, err error, start time.Time) {
	if r.history == nil || target == "" {
		return
	}
	result := "success"
	if err != nil {
		result = err.Error()
	}
	_ = r.history.Record(history.Operation{
		Timestamp:  start,
		Tool:       name,
		Type:       history.ToolToOperationType(name),
		Params:     args,
		Target:     target,
		BackupPath: backup,
		Success:    err == nil,
		Result:     result,
	})
}

// sortedNames is Names sorted, for displays.
func (r *Registry) sortedNames() []string {
	names := r.Names()
	sort.Strings(names)
	return names
}
