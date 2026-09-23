// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/ui"
)

// cmdMode is the /mode command: show or switch the operating mode.
func (r *repl) cmdMode(args []string) {
	if len(args) == 0 {
		fmt.Printf("Mode: %s (%d tools exposed)\n", r.asst.Mode(), r.asst.ToolRegistry().Available(r.asst.Mode()))
		if err := r.asst.PolicyError(); err != nil {
			fmt.Printf("Operate mode is locked: %v\n", err)
		}
		fmt.Println("Usage: /mode investigate | operate")
		fmt.Println()
		return
	}
	mode, ok := policy.ParseMode(args[0])
	if !ok {
		fmt.Printf("Unknown mode %q (investigate or operate)\n\n", args[0])
		return
	}
	if err := r.asst.SetMode(mode); err != nil {
		fmt.Printf("%s %v\n\n", ui.IconError, err)
		return
	}
	fmt.Printf("%s Mode: %s (%d tools exposed)\n\n", ui.IconSuccess, mode, r.asst.ToolRegistry().Available(mode))
	r.refreshPrompt()
}

// cmdPolicy is the /policy command: the effective policy (built-in, global and project files
// merged), where it came from, and any remembered per-tool permissions.
func (r *repl) cmdPolicy(args []string) {
	if len(args) == 0 || args[0] != "show" {
		fmt.Println("Usage: /policy show")
		fmt.Println()
		return
	}
	if err := r.asst.PolicyError(); err != nil {
		fmt.Printf("%s Policy error (operate mode locked): %v\n\n", ui.IconError, err)
		return
	}
	fmt.Printf("Sources: %s\n", strings.Join(r.asst.PolicySources(), ", "))
	data, err := yaml.Marshal(r.asst.Policy())
	if err != nil {
		fmt.Printf("%s %v\n\n", ui.IconError, err)
		return
	}
	fmt.Println(strings.TrimSpace(string(data)))
	if perms := r.asst.Permissions(); perms != nil && len(perms.Rules()) > 0 {
		fmt.Println("Remembered permissions:")
		for tool, perm := range perms.Rules() {
			fmt.Printf("  %-14s %s\n", tool, perm)
		}
	}
	fmt.Println()
}

// cmdPermissions is the /permissions command: the remembered answers for mutations.
func (r *repl) cmdPermissions(args []string) {
	perms := r.asst.Permissions()
	if perms == nil {
		fmt.Println("No permission store: run /init first.")
		fmt.Println()
		return
	}
	if len(args) == 0 {
		fmt.Println("Permission rules for mutations (reads never ask; default: ask):")
		for tool, perm := range perms.Rules() {
			fmt.Printf("  %-14s %s\n", tool, perm)
		}
		fmt.Println("  /permissions allow|deny|ask <tool|all>   /permissions reset")
		fmt.Println()
		return
	}
	if args[0] == "reset" {
		if err := perms.Reset(); err != nil {
			fmt.Printf("%s %v\n\n", ui.IconError, err)
			return
		}
		fmt.Println("Permissions reset: every mutation asks again.")
		fmt.Println()
		return
	}
	perm, ok := policy.ParsePermission(args[0])
	if !ok || len(args) < 2 {
		fmt.Println("Usage: /permissions allow|deny|ask <tool|all> | reset")
		fmt.Println()
		return
	}
	target := args[1]
	if target == "all" {
		target = "*"
	} else if _, known := r.asst.ToolRegistry().Get(target); !known {
		fmt.Printf("Unknown tool %q; see /tools\n\n", target)
		return
	}
	if err := perms.Set(target, perm); err != nil {
		fmt.Printf("%s %v\n\n", ui.IconError, err)
		return
	}
	fmt.Printf("%s %s -> %s\n\n", ui.IconSuccess, args[1], perm)
}

// cmdAudit is the /audit command: the mutations recorded in this project's audit log.
func (r *repl) cmdAudit(args []string) {
	st := r.asst.GetStorage()
	if st == nil {
		fmt.Println("No audit log: run /init first.")
		fmt.Println()
		return
	}
	sessionID := ""
	if s := r.asst.GetSession(); s != nil {
		sessionID = s.ID
	}
	switch {
	case len(args) >= 1 && args[0] == "clear":
		if err := r.asst.ClearAudit(); err != nil {
			fmt.Printf("%s %v\n\n", ui.IconError, err)
			return
		}
		fmt.Println("Audit log cleared.")
	case len(args) >= 2 && args[0] == "export" && args[1] == "json":
		recs, err := st.ReadAudit("")
		if err != nil {
			fmt.Printf("%s %v\n\n", ui.IconError, err)
			return
		}
		data, _ := json.MarshalIndent(recs, "", "  ")
		name := fmt.Sprintf("taracode-audit-%s.json", time.Now().Format("20060102-150405"))
		if err := os.WriteFile(name, data, 0o600); err != nil {
			fmt.Printf("%s %v\n\n", ui.IconError, err)
			return
		}
		fmt.Printf("Exported %d records to %s\n", len(recs), name)
	default:
		if len(args) >= 1 && args[0] == "all" {
			sessionID = ""
		}
		recs, err := st.ReadAudit(sessionID)
		if err != nil {
			fmt.Printf("%s %v\n\n", ui.IconError, err)
			return
		}
		if len(recs) == 0 && sessionID != "" {
			fmt.Println("No mutations recorded in this session (/audit all shows every session).")
		} else if len(recs) == 0 {
			fmt.Println("No mutations recorded.")
		}
		for _, rec := range recs {
			what := rec.Command
			if what == "" {
				what = rec.Targets["paths"]
			}
			fmt.Printf("  %s  %-5s %-22s %-10s %s\n", rec.Time.Format("15:04:05"), rec.Decision, rec.Rule, rec.Tool, what)
		}
	}
	fmt.Println()
}
