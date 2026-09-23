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
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/ui"
	"github.com/tara-vision/taracode/internal/upgrade"
)

// repl is the state of one interactive session: the assistant, the managers that exist once a
// project is initialised, the host pool and the readline instance. Command handlers are methods
// on it, so re-creating the assistant (/init, /model, /reload, /clear) updates one field.
type repl struct {
	asst      *assistant.Assistant
	renderer  *ui.Renderer
	rl        *readline.Instance
	completer *SlashCompleter

	host, apiKey, model, vendor string // the startup connection, reused by /reload and /clear
	streaming, spinner          bool

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

// connectionConfig resolves host, key, model and vendor from the flags, the config file and the
// hosts: block (moved from the top of the old startREPL, lines 33-63).
func connectionConfig() (host, apiKey, modelName, vendor string, multiHost bool) {
	hostsCfg := GetHostsConfig()
	multiHost = !hostsCfg.IsEmpty() && len(hostsCfg.Hosts) > 1
	host = viper.GetString("host")
	apiKey = viper.GetString("key")
	modelName = model // the --model flag; model: is a config section in 2.x config files
	vendor = viper.GetString("vendor")
	if !multiHost {
		return host, apiKey, modelName, vendor, false
	}
	if defaultHost, ok := hostsCfg.GetDefaultHost(); ok {
		if host == "" {
			host = defaultHost.URL
		}
		if apiKey == "" && defaultHost.APIKey != "" {
			apiKey = defaultHost.APIKey
		}
		if vendor == "" && defaultHost.Vendor != "" {
			vendor = defaultHost.Vendor
		}
		if modelName == "" && len(defaultHost.Models) > 0 {
			modelName = defaultHost.Models[0]
		}
	}
	return host, apiKey, modelName, vendor, true
}

// newREPL builds the session in the order the old startREPL did: connection, assistant (with the
// tool wiring), banner, mode, project managers, update check, MCP, host pool, readline.
func newREPL() (*repl, error) {
	host, apiKey, modelName, vendor, multiHost := connectionConfig()
	if host == "" {
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
	r := &repl{
		renderer:    ui.NewRenderer(),
		host:        host,
		apiKey:      apiKey,
		model:       modelName,
		vendor:      vendor,
		streaming:   !viper.GetBool("no_stream"),
		spinner:     !viper.GetBool("no_spinner"),
		projectRoot: workingDir,
		absDir:      workingDir,
		initialised: isInitializedProject(workingDir),
		updates:     make(chan *upgrade.CheckResult, 1),
	}
	asst, err := assistant.New(host, apiKey, modelName, vendor, r.streaming, r.spinner, toolConfig(r.renderer))
	if err != nil {
		return nil, fmt.Errorf("%s", ui.FormatConnectionError(host, err))
	}
	r.asst = asst
	r.printBanner()      // moved: provider message, session resume message (113-121)
	r.applyInitialMode() // moved: viper "mode" -> asst.SetMode with the warning (124-128)
	r.printWelcome()     // moved: WelcomeMessage, ProjectContextMessage (137-138)
	if r.initialised {
		r.enableProject()
	} else {
		r.printNotInitialised() // moved: the yellow "Project Not Initialized" box (141-155)
	}
	if viper.GetBool("upgrade.auto_check") {
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
		Prompt:          FormatPrompt(r.relDir),
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

// applyInitialMode sets the mode from the config/flag, warning (not failing) on an invalid value
// (moved from the old startREPL, lines 121-126).
func (r *repl) applyInitialMode() {
	initialMode := viper.GetString("mode")
	if initialMode == "" {
		return
	}
	mode, ok := policy.ParseMode(initialMode)
	if !ok {
		fmt.Fprintf(os.Stderr, "Warning: invalid mode %q (investigate or operate), using default mode\n", initialMode)
		return
	}
	if err := r.asst.SetMode(mode); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v, using default mode\n", err)
	}
}

// printWelcome shows the welcome message and the project-context line (moved from the old
// startREPL, lines 134-138).
func (r *repl) printWelcome() {
	fmt.Print(r.renderer.WelcomeMessage())
	fmt.Print(r.renderer.ProjectContextMessage(r.initialised))
}

// printNotInitialised shows the "Project Not Initialized" box and the commands available before
// /init (moved from the old startREPL, lines 139-153).
func (r *repl) printNotInitialised() {
	fmt.Println()
	fmt.Println("\033[33m┌─────────────────────────────────────────────────────┐\033[0m")
	fmt.Println("\033[33m│  Project Not Initialized                            │\033[0m")
	fmt.Println("\033[33m└─────────────────────────────────────────────────────┘\033[0m")
	fmt.Println()
	fmt.Println("  Run /init to initialize and enable:")
	fmt.Println("    - @ file references with Tab completion")
	fmt.Println("    - Project context awareness")
	fmt.Println("    - Session persistence")
	fmt.Println("    - Directory navigation (cd)")
	fmt.Println()
	fmt.Println("  Available commands: /init, /help, exit")
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
