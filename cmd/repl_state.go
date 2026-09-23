package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chzyer/readline"
	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/mcp"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/ui"
	"github.com/tara-vision/taracode/internal/upgrade"
)

// repl is the state of one interactive session: the assistant, the managers that exist once a
// project is initialised, the host pool and the readline instance. Command handlers are methods
// on it, so re-creating the assistant (/init, /model, /reload, /clear) goes through replaceAssistant.
type repl struct {
	asst      *assistant.Assistant
	renderer  *ui.Renderer
	rl        *readline.Instance
	completer *SlashCompleter

	opts assistant.Options // the startup connection and settings, reused by /reload and /clear

	projectRoot string // fixed at startup; the sandbox root
	relDir      string // current directory relative to projectRoot ("" = root)
	absDir      string // current absolute directory
	initialised bool

	history  *history.Manager
	memory   *memory.Manager
	mcp      *mcp.Manager
	hostPool *provider.HostPool
	updates  chan *upgrade.CheckResult
}

// options returns the settings a freshly re-created assistant should use: r.opts with Mode pinned
// to whatever mode the live assistant is in, so /init, /reload and /clear do not silently drop
// back to investigate mode.
func (r *repl) options() assistant.Options {
	opts := r.opts
	if r.asst != nil {
		opts.Mode = r.asst.Mode()
	}
	return opts
}

// replaceAssistant swaps in a freshly built assistant and re-wires everything assistant.New does
// not know about on its own: the history manager and the tools of every connected MCP server (the
// new assistant has a brand new, empty tool registry). Every /init, /reload, /clear and /model
// re-creation goes through this one helper, so neither wiring can be silently dropped at one call
// site while staying wired at another (ruling P2-R18; both were lost on every re-creation in v2).
func (r *repl) replaceAssistant(newAsst *assistant.Assistant) {
	r.asst = newAsst
	registry := r.asst.ToolRegistry()
	if r.history != nil {
		registry.SetHistory(r.history)
	}
	if r.mcp != nil {
		for _, tool := range r.mcp.GetAllTools() {
			registry.RegisterMCP(mcp.ToTool(r.mcp, tool), tool.ServerName)
		}
	}
	r.asst.RefreshTools()
	r.refreshPrompt()
}

// newREPL builds the session in the order the old startREPL did: connection, assistant (with the
// tool wiring), banner, mode, project managers, update check, MCP, host pool, readline.
func newREPL() (*repl, error) {
	opts, warnings := loadOptions()
	hostsCfg := GetHostsConfig()
	multiHost := !hostsCfg.IsEmpty() && len(hostsCfg.Hosts) > 1
	if multiHost {
		if defaultHost, ok := hostsCfg.GetDefaultHost(); ok {
			if opts.Host == "" {
				opts.Host = defaultHost.URL
			}
			if opts.APIKey == "" && defaultHost.APIKey != "" {
				opts.APIKey = defaultHost.APIKey
			}
			if opts.Vendor == "" && defaultHost.Vendor != "" {
				opts.Vendor = defaultHost.Vendor
			}
			if opts.Model == "" && len(defaultHost.Models) > 0 {
				opts.Model = defaultHost.Models[0]
			}
		}
	}
	if opts.Host == "" {
		return nil, fmt.Errorf("LLM server host not found.\nSet it via:\n" +
			"  - Environment variable: export TARACODE_HOST=http://localhost:11434\n" +
			"  - Config file: ~/.taracode/config.yaml\n" +
			"  - Command flag: --host http://localhost:11434\n" +
			"  - Multi-host config: hosts: section in config.yaml")
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	renderer := ui.NewRenderer()
	toolCfg := toolConfig(renderer)
	opts.Tools.Stream = toolCfg.Stream
	opts.Tools.Search = toolCfg.Search
	opts.WorkingDir = workingDir
	initialised := isInitializedProject(workingDir)
	opts.Ephemeral = !initialised

	r := &repl{
		renderer:    renderer,
		opts:        opts,
		projectRoot: workingDir,
		absDir:      workingDir,
		initialised: initialised,
		updates:     make(chan *upgrade.CheckResult, 1),
	}
	asst, err := assistant.New(r.opts)
	if err != nil {
		return nil, fmt.Errorf("%s", ui.FormatConnectionError(opts.Host, err))
	}
	r.asst = asst
	r.printBanner()         // moved: provider message, session resume message (113-121)
	printWarnings(warnings) // the 2.x config migration warnings loadOptions collected
	r.printWelcome()        // moved: WelcomeMessage, ProjectContextMessage (137-138)
	if r.initialised {
		r.enableProject()
	} else {
		r.printNotInitialised() // moved: the yellow "Project Not Initialized" box (141-155)
	}
	if viper.GetBool("upgrade.auto_check") && !opts.Offline {
		CheckForUpdateAsync(Version, r.updates)
	}
	r.startMCP() // moved: 199-219, callback registers into r.asst at call time
	if multiHost {
		r.startHostPool() // moved: 225-245 (NewHostPool, ConnectAll 60 s, StartHealthChecks, count line, SetHostPool)
	}
	if err := r.openReadline(); err != nil {
		return nil, err
	}
	select {
	case result := <-r.updates:
		ShowUpdateBanner(result)
	default:
	}
	return r, nil
}

// enableProject creates the managers that need .taracode/: history (wired into the tool registry),
// memory, and the MCP auto-connect. Called at startup for an initialised project and after /init.
// The bodies are the old startup blocks 158-188 and the /init path 436-462.
func (r *repl) enableProject() {
	r.initialised = true
	taracodeDir := filepath.Join(r.projectRoot, ".taracode")
	if r.history == nil {
		sessionID := "default"
		if session := r.asst.GetSession(); session != nil {
			sessionID = session.ID
		}
		if hm, err := history.NewManager(taracodeDir, sessionID); err == nil {
			r.history = hm
			if registry := r.asst.ToolRegistry(); registry != nil {
				registry.SetHistory(hm)
			}
		}
	}
	if r.memory == nil {
		if mm, err := memory.NewManager(taracodeDir); err == nil {
			r.memory = mm
			// keep the old "Loaded %d project memories" line exactly as startREPL printed it
			if count := mm.Count(); count > 0 {
				fmt.Printf("%s Loaded %d project memories\n", ui.SuccessStyle.Render(ui.IconSuccess), count)
			}
		}
	}
	if r.mcp != nil {
		r.mcp.AutoConnect(context.Background())
	}
}

// startMCP builds the MCP manager and registers discovered tools into whichever assistant is
// current when a server connects.
func (r *repl) startMCP() {
	cfg := GetMCPConfig()
	if !cfg.Enabled || len(cfg.Servers) == 0 {
		return
	}
	r.mcp = mcp.NewManager(cfg)
	r.mcp.SetToolDiscoveryCallback(func(_ string, tools []mcp.MCPTool) {
		registry := r.asst.ToolRegistry()
		for _, tool := range tools {
			registry.RegisterMCP(mcp.ToTool(r.mcp, tool), tool.ServerName)
		}
		r.asst.RefreshTools()
	})
	if r.initialised {
		r.mcp.AutoConnect(context.Background())
	}
}

// openReadline sets up the input line with slash and @file completion.
func (r *repl) openReadline() error {
	r.completer = NewSlashCompleter(r.absDir)
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          r.formatPrompt(),
		HistoryFile:     filepath.Join(os.Getenv("HOME"), ".taracode", "history"),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    r.completer,
	})
	if err != nil {
		return fmt.Errorf("setting up readline: %w", err)
	}
	r.rl = rl
	return nil
}

// close releases the host pool and the terminal.
func (r *repl) close() {
	if r.hostPool != nil {
		r.hostPool.Close()
	}
	if r.rl != nil {
		_ = r.rl.Close()
	}
}

// printBanner shows the provider info and, when resuming a session, the resume message (moved
// from the top of the old startREPL, lines 110-119).
func (r *repl) printBanner() {
	if providerInfo := r.asst.GetProviderInfo(); providerInfo != nil {
		fmt.Print(r.renderer.ProviderMessage(providerInfo))
	}
	if session := r.asst.GetSession(); session != nil && len(session.Messages) > 0 {
		fmt.Print(r.renderer.SessionResumeMessage(len(session.Messages)))
	}
	fmt.Println()
}

// printWelcome shows the welcome message, the project-context line (moved from the old startREPL,
// lines 134-138) and the active mode: the REPL now works before /init too, so the welcome banner is
// where a session first learns whether it started in investigate or operate mode.
func (r *repl) printWelcome() {
	fmt.Print(r.renderer.WelcomeMessage())
	fmt.Print(r.renderer.ProjectContextMessage(r.initialised))
	fmt.Printf("Mode: %s (%d tools)\n", r.asst.Mode(), r.asst.ToolRegistry().Available(r.asst.Mode()))
}

// printNotInitialised shows the one-line notice that nothing is persisted until /init runs. The REPL
// itself is fully usable before /init (an ephemeral assistant with nothing saved), so this replaces
// the old multi-line "Project Not Initialized" box that listed features as unavailable.
func (r *repl) printNotInitialised() {
	fmt.Println("Not initialised: nothing is saved (sessions, memory, history off); " +
		"run /init to enable them and operate mode.")
	fmt.Println()
}

// startHostPool connects the configured hosts, starts background health checks and wires the pool
// into the assistant for automatic fallback (moved from the old startREPL, lines 219-240).
func (r *repl) startHostPool() {
	hostsCfg := GetHostsConfig()
	r.hostPool = provider.NewHostPool(hostsCfg)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := r.hostPool.ConnectAll(connectCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: some hosts failed to connect: %v\n", err)
	}
	connectCancel()

	r.hostPool.StartHealthChecks(context.Background())

	fmt.Printf("%s Multi-host mode: %d/%d hosts connected\n",
		ui.SuccessStyle.Render(ui.IconSuccess),
		r.hostPool.HealthyCount(),
		r.hostPool.HostCount())

	r.asst.SetHostPool(r.hostPool)
}
