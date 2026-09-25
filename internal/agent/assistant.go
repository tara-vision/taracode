// Package agent is the agentic loop: prompt, turn, tool gate, compaction, sessions.
package agent

import (
	gocontext "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/context"
	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/tools/redact"
	"github.com/tara-vision/taracode/internal/ui"
)

// Timeout and retry constants
const (
	providerInitTimeout      = 2 * time.Minute // Timeout for provider initialization with retries
	apiResponseTimeout       = 5 * time.Minute
	modelOperationTimeout    = 30 * time.Second // Timeout for model list/switch operations
	defaultMaxToolIterations = 20               // Default max tool call iterations before stopping
)

// Assistant is the main agent loop controller managing LLM interactions, tool calls, and sessions.
type Assistant struct {
	provider      provider.Provider
	llm           llm.Client
	model         string
	conversation  []openai.ChatCompletionMessage
	toolRegistry  *tools.Registry
	toolDefs      []openai.Tool // the schemas the current mode exposes, refreshed from the registry
	workingDir    string
	streaming     bool // Enable streaming output (default: true)
	enableSpinner bool // Enable spinner animations (default: true)
	renderer      *ui.Renderer

	// Persistence fields
	storage    *storage.Manager
	session    *storage.Session
	projectCtx *context.ProjectContext

	// Token usage tracking
	sessionUsage *storage.TokenUsage

	// Operating mode (investigate, operate) and the policy gate
	mode          policy.Mode
	systemPrompt  string
	pol           policy.Policy
	policySources []string
	policyErr     error // set when a policy file failed to load; the session is locked to investigate
	permissions   *policy.Permissions
	redactor      *redact.Redactor

	// confirmPermission asks whether one mutate invocation may run. It is the terminal prompt in the
	// binary and is replaced in tests so the decision is deterministic without a TTY.
	confirmPermission func(inv policy.Invocation, args map[string]any) ui.PermissionChoice

	// confirmEditPreview decides an edit preview. It is the terminal prompt in the binary and is
	// replaced in tests so the decision is deterministic without a TTY.
	confirmEditPreview func(preview *ui.EditPreview) ui.EditPreviewChoice

	// Last AI response (for suggestion detection)
	lastResponse string

	// Ollama context window check (v2.1.0)
	serverContextChecked bool // true once /api/ps has answered for the current model
	serverContextTokens  int  // context window Ollama loaded the model with (0 = unknown)

	// Request options sent with every turn (Task 8 wires these to config)
	think             llm.Think // reasoning mode
	thinkingSupported bool      // whether the current model has the "thinking" capability; true when unknown
	keepAlive         string    // how long the server keeps the model loaded
	contextWindow     int       // num_ctx for the request, 0 = server default
	configuredWindow  string    // context.window config value ("auto" or a number); re-read on model switch

	// Context management (v2.0.2)
	truncationCfg   TruncationConfig
	compactionCfg   CompactionConfig
	compactionState *CompactionState
	maxIterations   int // Configurable max tool iterations per message
	modelOptions    ModelOptions

	// Edit preview and memory settings (Task 13: from Options, no viper reads at call time)
	previewEdits     bool
	previewThreshold int
	memoryEnabled    bool
	memoryMaxTokens  int

	// Truncation tracking for /context display
	truncationEvents []TruncationResult

	// Headless hooks (Phase 3): where the loop prints, who observes tool decisions, the last turn.
	out      io.Writer
	observer func(ToolEvent)
	turn     TurnStats
}

// stdoutWriter is the output when Options.Output is nil. It writes to os.Stdout as it is at the time
// of each write, as the bare prints it replaced did, so a caller that swaps os.Stdout after New (the
// tests capture output that way) still gets what the assistant prints.
type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// New connects to the LLM server, picks the model, loads the policy, the permission store and the
// redactor, builds the tool registry and the system prompt. Every setting comes from opts: no
// package under internal/ reads viper (cmd's loadOptions does, with the 2.x migrations).
func New(opts Options) (*Assistant, error) {
	renderer := ui.NewRenderer()
	out := opts.Output
	if out == nil {
		out = stdoutWriter{}
	}

	// Create context with timeout for provider initialization
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), providerInitTimeout)
	defer cancel()

	// Create provider (auto-detects vendor if not specified)
	prov, err := provider.New(ctx, opts.Host, opts.Vendor, opts.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	workingDir := opts.WorkingDir
	if workingDir == "" {
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Initialize storage manager first (to load persisted model preference). Ephemeral sessions
	// (opts.Ephemeral) skip this entirely: no session, no permissions, no warning about it - the
	// caller asked for nothing to be persisted.
	var storageMgr *storage.Manager
	var session *storage.Session
	var projectCtx *context.ProjectContext
	var persistedModel string

	if !opts.Ephemeral {
		storageMgr, err = storage.NewManager(workingDir)
		if err != nil {
			// Storage initialization failed - continue without persistence
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(fmt.Sprintf("Could not initialize storage: %v", err)))
		} else {
			// Load persisted model preference
			persistedModel = storageMgr.GetPreferredModel()

			// Try to load or create active session
			session, _ = storageMgr.GetActiveSession()
			if session == nil {
				session, _ = storageMgr.CreateSession("")
			}

			// Load project context if available
			projectCtx, _ = storageMgr.LoadProjectContext()
		}
	}

	gate := loadGate(workingDir, storageMgr, renderer, out)
	registry := tools.NewBuiltinRegistry(
		tools.Options{Offline: opts.Offline, Redactor: gate.redactor, Middleware: opts.ToolMiddleware}, opts.Tools)

	// Determine which model to use (priority: persisted > config > auto-detect)
	models, detectErr := prov.DetectModels(ctx)
	model, err := chooseModel(models, detectErr, persistedModel, opts.Model, renderer, out)
	if err != nil {
		return nil, err
	}

	// Update provider with selected model
	prov.SetModel(model)

	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxToolIterations
	}
	if maxIter > 50 {
		maxIter = 50 // cap
	}

	compactionCfg := opts.Compaction
	if compactionCfg.MaxTokens <= 0 {
		compactionCfg.MaxTokens = opts.MaxContextTokens
	}
	// A 2.x config (or any caller) can hand New an out-of-range threshold (2.x examples used
	// percent-like values such as 75 instead of 0.75) or a non-positive keep-recent; both must be
	// clamped here; an unclamped negative KeepRecent inflates CompactConversation's keepEnd past
	// len(conversation) and panics the first time compaction fires (fix review finding 1).
	if compactionCfg.Threshold <= 0 || compactionCfg.Threshold > 1.0 {
		compactionCfg.Threshold = 0.75
	}
	if compactionCfg.KeepRecent <= 0 {
		compactionCfg.KeepRecent = 4
	}

	// Request options (Task 8): reasoning mode, context window, keep-alive. An unrecognized think
	// value falls back to auto rather than failing the whole assistant.
	think, thinkOK := llm.ParseThink(opts.Think)
	if !thinkOK {
		_, _ = fmt.Fprintln(out, renderer.WarningMessage(fmt.Sprintf("think %q not recognized; using auto", opts.Think)))
	}

	a := &Assistant{
		provider:           prov,
		llm:                prov.LLM(),
		model:              model,
		confirmEditPreview: ui.DisplayEditPreview,
		confirmPermission:  ui.PromptPermission,
		toolRegistry:       registry,
		pol:                gate.pol,
		policySources:      gate.sources,
		policyErr:          gate.err,
		permissions:        gate.permissions,
		redactor:           gate.redactor,
		mode:               policy.ModeInvestigate,
		workingDir:         workingDir,
		streaming:          opts.Streaming,
		enableSpinner:      opts.Spinner,
		renderer:           renderer,
		storage:            storageMgr,
		session:            session,
		projectCtx:         projectCtx,
		sessionUsage:       &storage.TokenUsage{},
		think:              think,
		keepAlive:          opts.KeepAlive,
		configuredWindow:   opts.ContextWindow,
		truncationCfg:      opts.Truncation,
		compactionCfg:      compactionCfg,
		compactionState:    NewCompactionState(),
		maxIterations:      maxIter,
		modelOptions:       opts.Generation,
		previewEdits:       opts.PreviewEdits,
		previewThreshold:   opts.PreviewThreshold,
		memoryEnabled:      opts.MemoryEnabled,
		memoryMaxTokens:    opts.MemoryMaxTokens,
		truncationEvents:   make([]TruncationResult, 0),
		out:                out,
		observer:           opts.ToolObserver,
	}
	if opts.PermissionDecider != nil {
		a.confirmPermission = opts.PermissionDecider
	}

	// Resolve the context window and gate on tool support once the model is chosen, before the
	// system prompt is built: an OpenAI-compatible server without Show just leaves the window
	// unresolved (v3 native core, Task 8).
	showCtx, showCancel := gocontext.WithTimeout(gocontext.Background(), 5*time.Second)
	defer showCancel()
	if err := a.applyModelDetails(a.llm.Show(showCtx, a.model)); err != nil {
		return nil, err
	}

	a.refreshTools()
	a.systemPrompt = buildSystemPrompt(workingDir, storageMgr, a.mode, a.memoryBudget())
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: a.systemPrompt,
	}}
	a.applyStartupMode(opts)

	return a, nil
}

// sameModel reports whether a and b name the same Ollama model once its implicit tag is accounted
// for: Ollama treats a name given without a tag as "name:latest" and lists it that way from
// /api/tags, so a bare "name" and a listed "name:latest" must compare equal (ruling P3-R74). Only
// one trailing ":latest" is stripped from each side (Ollama never double-tags a real model name),
// so "x:latest:latest" is compared as "x:latest", not collapsed any further.
func sameModel(a, b string) bool {
	return strings.TrimSuffix(a, ":latest") == strings.TrimSuffix(b, ":latest")
}

// chooseModel picks the model New uses: the persisted one while the server still lists it, then the
// configured one, then the first the server lists. When the server cannot list its models the
// persisted or configured name is trusted.
func chooseModel(
	models []string, detectErr error, persistedModel, configModel string, renderer *ui.Renderer, out io.Writer,
) (string, error) {
	var model string

	// Helper to check if model exists in available models
	modelAvailable := func(name string) bool {
		for _, m := range models {
			if sameModel(m, name) {
				return true
			}
		}
		return false
	}

	if detectErr != nil {
		// Can't detect models - use persisted or config
		if persistedModel != "" {
			model = persistedModel
			_, _ = fmt.Fprintln(out, renderer.SuccessMessage(fmt.Sprintf("Using saved model: %s", model)))
		} else if configModel != "" {
			model = configModel
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(
				fmt.Sprintf("Could not detect models (%v), using configured: %s", detectErr, model)))
		} else {
			return "", fmt.Errorf("failed to detect models and no fallback configured: %w", detectErr)
		}
	} else if len(models) > 0 {
		// Models available - check persisted, then config, then first available
		if persistedModel != "" && modelAvailable(persistedModel) {
			model = persistedModel
			_, _ = fmt.Fprintln(out, renderer.SuccessMessage(fmt.Sprintf("Using saved model: %s", model)))
		} else if persistedModel != "" && !modelAvailable(persistedModel) {
			// Saved model no longer available - warn and use first available
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(
				fmt.Sprintf("Saved model '%s' not available. Use /model to select.", persistedModel)))
			model = models[0]
			_, _ = fmt.Fprintln(out, renderer.SuccessMessage(fmt.Sprintf("Using: %s", model)))
		} else if configModel != "" && modelAvailable(configModel) {
			model = configModel
			_, _ = fmt.Fprintln(out, renderer.SuccessMessage(fmt.Sprintf("Using configured model: %s", model)))
		} else if configModel != "" {
			model = models[0]
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(
				fmt.Sprintf("Configured model '%s' not available. Using: %s", configModel, model)))
		} else {
			model = models[0]
			_, _ = fmt.Fprintln(out, renderer.SuccessMessage(fmt.Sprintf("Using model: %s", model)))
		}
	} else {
		if persistedModel != "" {
			model = persistedModel
		} else if configModel != "" {
			model = configModel
		} else {
			// No model persisted or configured, and the server lists none either: point at the
			// registry's recommendation for this host before failing (native core, Task 11).
			printFirstRunAdvice(out)
			return "", fmt.Errorf("no models available and no fallback configured")
		}
	}

	return model, nil
}

// gateConfig is what New loads for the policy gate.
type gateConfig struct {
	pol         policy.Policy // the built-in policy when a policy file failed to load
	sources     []string
	err         error // why a policy file failed to load; the session is then locked to investigate
	permissions *policy.Permissions
	redactor    *redact.Redactor
}

// loadGate loads the policy for workingDir, the permission store from the project storage and the
// redactor the policy asks for. Every problem is reported and degrades to a safe default.
func loadGate(workingDir string, storageMgr *storage.Manager, renderer *ui.Renderer, out io.Writer) gateConfig {
	var g gateConfig
	var err error
	home, _ := os.UserHomeDir()
	g.pol, g.sources, g.err = policy.Load(workingDir, home)
	if g.err != nil {
		_, _ = fmt.Fprintln(out, renderer.WarningMessage(
			fmt.Sprintf("Policy error, session locked to investigate mode: %v", g.err)))
		g.pol = policy.Default()
	}
	if storageMgr != nil {
		var ignored bool
		g.permissions, ignored, err = policy.LoadPermissions(filepath.Join(storageMgr.GetRootDir(), "permissions.json"))
		if err != nil {
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(fmt.Sprintf("Permissions file ignored: %v", err)))
			g.permissions, _, _ = policy.LoadPermissions("")
		} else if ignored {
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(
				"permissions.json is from taracode 2.x and was ignored; rules are per tool now (/permissions)"))
		}
	}
	if g.pol.RedactEnabled() {
		g.redactor, err = redact.New(redact.Options{ExtraPatterns: g.pol.Redact.ExtraPatterns, Environ: os.Environ()})
		if err != nil {
			_, _ = fmt.Fprintln(out, renderer.WarningMessage(fmt.Sprintf("Redaction extra pattern ignored: %v", err)))
			g.redactor, _ = redact.New(redact.Options{Environ: os.Environ()})
		}
	}
	return g
}

// newForTest builds an Assistant on a fake server without storage, spinner or interactive
// prompts, with the same setting values DefaultOptions gives the binary. Auto-compaction is kept
// off (ForceCompact is exercised directly where a test cares about it), so an ordinary test never
// triggers a surprise summarization call. Test-only: New is the constructor the binary uses.
func newForTest(workingDir, model, host string, streaming bool) *Assistant {
	prov := provider.NewOllamaProvider(host, "")
	prov.SetModel(model)
	// The built-in policy has redaction on; the test redactor leaves the environment out so what a
	// test sees does not depend on the machine's variables.
	red, _ := redact.New(redact.Options{})
	opts := DefaultOptions()
	a := &Assistant{
		provider:           prov,
		llm:                prov.LLM(),
		model:              model,
		confirmEditPreview: ui.DisplayEditPreview,
		confirmPermission: func(policy.Invocation, map[string]any) ui.PermissionChoice {
			return ui.PermissionChoice{Allowed: true}
		},
		workingDir:        workingDir,
		streaming:         streaming,
		enableSpinner:     false,
		renderer:          ui.NewRenderer(),
		toolRegistry:      tools.NewBuiltinRegistry(tools.Options{Redactor: red}, tools.Config{}),
		pol:               policy.Default(),
		permissions:       policy.AllowAll(),
		redactor:          red,
		sessionUsage:      &storage.TokenUsage{},
		mode:              policy.ModeInvestigate,
		contextWindow:     32768,
		thinkingSupported: true, // no applyModelDetails call in tests; capabilities are unknown
		truncationCfg:     opts.Truncation,
		compactionCfg: CompactionConfig{
			Enabled:    false, // auto-compact off in tests; ForceCompact is exercised directly
			Threshold:  opts.Compaction.Threshold,
			KeepRecent: opts.Compaction.KeepRecent,
			MaxTokens:  opts.Compaction.MaxTokens,
		},
		compactionState:  NewCompactionState(),
		maxIterations:    opts.MaxIterations,
		modelOptions:     opts.Generation,
		previewEdits:     opts.PreviewEdits,
		previewThreshold: opts.PreviewThreshold,
		memoryEnabled:    opts.MemoryEnabled,
		memoryMaxTokens:  opts.MemoryMaxTokens,
		truncationEvents: make([]TruncationResult, 0),
		out:              stdoutWriter{},
	}
	a.refreshTools()
	a.systemPrompt = buildSystemPrompt(workingDir, nil, a.mode, a.memoryBudget())
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: a.systemPrompt,
	}}
	return a
}

// refreshTools re-reads the tool schemas the current mode exposes.
func (a *Assistant) refreshTools() { a.toolDefs = a.toolRegistry.Definitions(a.mode) }

// RefreshTools is refreshTools for callers that changed the registry (MCP connect/disconnect).
func (a *Assistant) RefreshTools() { a.refreshTools() }

// ToolRegistry exposes the registry for MCP registration and /tools.
func (a *Assistant) ToolRegistry() *tools.Registry { return a.toolRegistry }

// Mode is the current operating mode.
func (a *Assistant) Mode() policy.Mode { return a.mode }

// Policy is the effective policy, PolicySources where it came from, PolicyError why it could not
// be loaded (the session is then locked to investigate mode).
func (a *Assistant) Policy() policy.Policy { return a.pol }

// PolicySources lists where the effective policy came from.
func (a *Assistant) PolicySources() []string { return a.policySources }

// PolicyError is why the policy could not be loaded; nil when it loaded.
func (a *Assistant) PolicyError() error { return a.policyErr }

// Permissions is the remembered answers for mutations; nil without project storage.
func (a *Assistant) Permissions() *policy.Permissions { return a.permissions }

// Redactions is the number of secret spans redacted from tool output so far.
func (a *Assistant) Redactions() int64 { return a.toolRegistry.Redactions() }

// ClearAudit deletes the audit log.
func (a *Assistant) ClearAudit() error {
	if a.storage == nil {
		return fmt.Errorf("no project storage (run /init)")
	}
	return a.storage.ClearAudit()
}

// GetProvider returns the LLM provider
func (a *Assistant) GetProvider() provider.Provider {
	return a.provider
}

// GetSessionUsage returns current token usage stats
func (a *Assistant) GetSessionUsage() *storage.TokenUsage {
	return a.sessionUsage
}

// GetConversationLength returns the number of messages in the conversation
func (a *Assistant) GetConversationLength() int {
	return len(a.conversation)
}

// GetLastResponse returns the last AI response text (for suggestion detection)
func (a *Assistant) GetLastResponse() string {
	return a.lastResponse
}

// GetProviderInfo returns information about the current LLM provider
func (a *Assistant) GetProviderInfo() *provider.Info {
	if a.provider == nil {
		return nil
	}
	return a.provider.Info()
}

// GetCurrentModel returns the current model name
func (a *Assistant) GetCurrentModel() string {
	return a.model
}

// GetProjectContext returns the project context if loaded
func (a *Assistant) GetProjectContext() *context.ProjectContext {
	return a.projectCtx
}

// ListModels returns available models from the provider (Ollama only)
func (a *Assistant) ListModels() ([]provider.ModelInfo, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("provider not initialized")
	}

	// Check if provider supports model management
	mm, ok := a.provider.(provider.ModelManager)
	if !ok {
		return nil, fmt.Errorf("provider does not support model listing (Ollama only)")
	}

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), modelOperationTimeout)
	defer cancel()

	return mm.ListModels(ctx)
}

// SwitchModel switches to a different model, unloading the current one first
func (a *Assistant) SwitchModel(newModel string) error {
	if a.provider == nil {
		return fmt.Errorf("provider not initialized")
	}

	// Check if provider supports model management
	mm, ok := a.provider.(provider.ModelManager)
	if !ok {
		return fmt.Errorf("provider does not support model switching (Ollama only)")
	}

	oldModel := a.model

	// Only unload if we're actually changing models
	if oldModel != "" && oldModel != newModel {
		ctx, cancel := gocontext.WithTimeout(gocontext.Background(), modelOperationTimeout)
		defer cancel()

		// Unload the old model to free memory
		if err := mm.UnloadModel(ctx, oldModel); err != nil {
			// Log but don't fail - the model might not be loaded
			_, _ = fmt.Fprintf(a.out, "  Note: Could not unload %s (may not be loaded)\n", oldModel)
		}
	}

	// Set the new model
	a.model = newModel
	a.resetServerContextCheck()
	a.provider.SetModel(newModel)

	// Resolve the new model's context window and gate on tool support (Task 8); a refused
	// switch restores the model the assistant already had loaded.
	showCtx, showCancel := gocontext.WithTimeout(gocontext.Background(), 5*time.Second)
	defer showCancel()
	if err := a.applyModelDetails(a.llm.Show(showCtx, a.model)); err != nil {
		a.model = oldModel
		a.provider.SetModel(oldModel)
		return err
	}

	// Persist the model selection
	if a.storage != nil {
		if err := a.storage.SetPreferredModel(newModel); err != nil {
			// Log but don't fail
			_, _ = fmt.Fprintf(a.out, "  Note: Could not save model preference: %v\n", err)
		}
	}

	return nil
}
