package assistant

import (
	gocontext "context"
	"fmt"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/context"
	"github.com/tara-vision/taracode/internal/legacytools"
	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// Timeout and retry constants
const (
	providerInitTimeout      = 2 * time.Minute // Timeout for provider initialization with retries
	apiResponseTimeout       = 5 * time.Minute
	modelOperationTimeout    = 30 * time.Second // Timeout for model list/switch operations
	defaultMaxToolIterations = 10               // Default max tool call iterations before stopping
)

type Assistant struct {
	provider      provider.Provider
	llm           llm.Client
	model         string
	conversation  []openai.ChatCompletionMessage
	toolRegistry  *legacytools.Registry
	toolDefs      []openai.Tool // OpenAI function calling tool definitions
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

	// Operating mode (devops, security)
	mode           storage.OperatingMode
	systemPrompt   string
	useNativeTools bool // true when model supports native function calling

	// Permission management
	permMgr *permissions.Manager

	// confirmEditPreview decides an edit preview. It is the terminal prompt in the binary and is
	// replaced in tests so the decision is deterministic without a TTY.
	confirmEditPreview func(preview *ui.EditPreview) ui.EditPreviewChoice

	// Last AI response (for suggestion detection)
	lastResponse string

	// Multi-host fallback support (v2.0)
	hostPool *provider.HostPool

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

	// Truncation tracking for /context display
	truncationEvents []TruncationResult
}

func New(host, apiKey, configModel, vendor string, streaming bool, enableSpinner bool) (*Assistant, error) {
	renderer := ui.NewRenderer()

	// Create context with timeout for provider initialization
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), providerInitTimeout)
	defer cancel()

	// Create provider (auto-detects vendor if not specified)
	prov, err := provider.New(ctx, host, vendor, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Initialize storage manager first (to load persisted model preference)
	var storageMgr *storage.Manager
	var session *storage.Session
	var projectCtx *context.ProjectContext
	var persistedModel string
	var permMgr *permissions.Manager

	storageMgr, err = storage.NewManager(workingDir)
	if err != nil {
		// Storage initialization failed - continue without persistence
		fmt.Println(renderer.WarningMessage(fmt.Sprintf("Could not initialize storage: %v", err)))
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

		// Initialize permission manager
		permMgr, _ = permissions.NewManager(workingDir)
	}

	// Determine which model to use (priority: persisted > config > auto-detect)
	models, err := prov.DetectModels(ctx)
	var model string

	// Helper to check if model exists in available models
	modelAvailable := func(name string) bool {
		for _, m := range models {
			if m == name {
				return true
			}
		}
		return false
	}

	if err != nil {
		// Can't detect models - use persisted or config
		if persistedModel != "" {
			model = persistedModel
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using saved model: %s", model)))
		} else if configModel != "" {
			model = configModel
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Could not detect models (%v), using configured: %s", err, model)))
		} else {
			return nil, fmt.Errorf("failed to detect models and no fallback configured: %w", err)
		}
	} else if len(models) > 0 {
		// Models available - check persisted, then config, then first available
		if persistedModel != "" && modelAvailable(persistedModel) {
			model = persistedModel
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using saved model: %s", model)))
		} else if persistedModel != "" && !modelAvailable(persistedModel) {
			// Saved model no longer available - warn and use first available
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Saved model '%s' not available. Use /model to select.", persistedModel)))
			model = models[0]
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using: %s", model)))
		} else if configModel != "" && modelAvailable(configModel) {
			model = configModel
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using configured model: %s", model)))
		} else if configModel != "" {
			model = models[0]
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Configured model '%s' not available. Using: %s", configModel, model)))
		} else {
			model = models[0]
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using model: %s", model)))
		}
	} else {
		if persistedModel != "" {
			model = persistedModel
		} else if configModel != "" {
			model = configModel
		} else {
			// No model persisted or configured, and the server lists none either: point at the
			// registry's recommendation for this host before failing (native core, Task 11).
			printFirstRunAdvice()
			return nil, fmt.Errorf("no models available and no fallback configured")
		}
	}

	// Update provider with selected model
	prov.SetModel(model)

	// Load context management configuration (v2.0.2)
	maxToolOutputLines := viper.GetInt("context.max_tool_output_lines")
	maxToolOutputChars := viper.GetInt("context.max_tool_output_chars")
	maxIter := viper.GetInt("context.max_tool_iterations")
	if maxIter <= 0 {
		maxIter = 10 // default
	}
	if maxIter > 50 {
		maxIter = 50 // cap
	}

	// Load model generation options (v2.0.4)
	modelOpts := ModelOptions{
		Temperature: float32(viper.GetFloat64("model.temperature")),
		TopP:        float32(viper.GetFloat64("model.top_p")),
		NumPredict:  viper.GetInt("model.num_predict"),
	}

	compactionEnabled := viper.GetBool("context.compaction_enabled") && !viper.GetBool("context.no_compaction")
	compactionThreshold := viper.GetFloat64("context.compaction_threshold")
	if compactionThreshold <= 0 || compactionThreshold > 1.0 {
		compactionThreshold = 0.75
	}
	compactionKeepRecent := viper.GetInt("context.compaction_keep_recent")
	if compactionKeepRecent <= 0 {
		compactionKeepRecent = 4
	}

	// Request options read from config (Task 8): reasoning mode, context window, keep-alive.
	// An unrecognized think value falls back to auto rather than failing the whole assistant.
	think, thinkOK := llm.ParseThink(viper.GetString("think"))
	if !thinkOK {
		fmt.Println(renderer.WarningMessage(
			fmt.Sprintf("think %q not recognized; using auto", viper.GetString("think"))))
	}

	a := &Assistant{
		provider:           prov,
		llm:                prov.LLM(),
		model:              model,
		confirmEditPreview: ui.DisplayEditPreview,
		toolRegistry:       legacytools.NewRegistry(),
		toolDefs:           legacytools.GetToolDefinitions(), // Initialize OpenAI function calling tools
		workingDir:         workingDir,
		streaming:          streaming,
		enableSpinner:      enableSpinner,
		renderer:           renderer,
		storage:            storageMgr,
		session:            session,
		projectCtx:         projectCtx,
		sessionUsage:       &storage.TokenUsage{},
		useNativeTools:     true, // Start with native tools enabled
		permMgr:            permMgr,
		think:              think,
		keepAlive:          viper.GetString("keep_alive"),
		configuredWindow:   viper.GetString("context.window"),
		truncationCfg: TruncationConfig{
			MaxLines: maxToolOutputLines,
			MaxChars: maxToolOutputChars,
		},
		compactionCfg: CompactionConfig{
			Enabled:    compactionEnabled,
			Threshold:  compactionThreshold,
			KeepRecent: compactionKeepRecent,
			MaxTokens:  viper.GetInt("max_context_tokens"),
		},
		compactionState:  NewCompactionState(),
		maxIterations:    maxIter,
		modelOptions:     modelOpts,
		truncationEvents: make([]TruncationResult, 0),
	}

	// Resolve the context window and gate on tool support once the model is chosen, before the
	// system prompt is built: an OpenAI-compatible server without Show just leaves the window
	// unresolved (v3 native core, Task 8).
	showCtx, showCancel := gocontext.WithTimeout(gocontext.Background(), 5*time.Second)
	defer showCancel()
	if err := a.applyModelDetails(a.llm.Show(showCtx, a.model)); err != nil {
		return nil, err
	}

	// Build system prompt - use compact version since we'll try native function calling first
	// If model doesn't support native tools, we'll rebuild with full prompt on fallback
	systemPrompt := buildSystemPromptWithModeAndTools(workingDir, storageMgr, storage.ModeDevOps, true)
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	return a, nil
}

// newForTest builds an Assistant on a fake server without storage, spinner or interactive
// prompts. Test-only: New is the constructor the binary uses.
func newForTest(workingDir, model, host string, streaming bool) *Assistant {
	prov := provider.NewOllamaProvider(host, "")
	prov.SetModel(model)
	a := &Assistant{
		provider:           prov,
		llm:                prov.LLM(),
		model:              model,
		confirmEditPreview: ui.DisplayEditPreview,
		workingDir:         workingDir,
		streaming:          streaming,
		enableSpinner:      false,
		renderer:           ui.NewRenderer(),
		toolRegistry:       legacytools.NewRegistry(),
		toolDefs:           legacytools.GetToolDefinitions(),
		sessionUsage:       &storage.TokenUsage{},
		mode:               storage.ModeDevOps,
		useNativeTools:     true,
		contextWindow:      32768,
		thinkingSupported:  true, // no applyModelDetails call in tests; capabilities are unknown
		truncationCfg: TruncationConfig{
			MaxLines: DefaultMaxToolOutputLines,
			MaxChars: DefaultMaxToolOutputChars,
		},
		compactionCfg: CompactionConfig{
			Enabled:    false,
			Threshold:  0.75,
			KeepRecent: 4,
			MaxTokens:  32768,
		},
		compactionState:  NewCompactionState(),
		maxIterations:    defaultMaxToolIterations,
		truncationEvents: make([]TruncationResult, 0),
	}
	a.systemPrompt = buildSystemPromptWithModeAndTools(workingDir, nil, a.mode, true)
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: a.systemPrompt,
	}}
	return a
}

// GetToolRegistry returns the tool registry
func (a *Assistant) GetToolRegistry() *legacytools.Registry {
	return a.toolRegistry
}

// GetProvider returns the LLM provider
func (a *Assistant) GetProvider() provider.Provider {
	return a.provider
}

// SetHostPool sets the host pool for multi-host fallback support (v2.0)
func (a *Assistant) SetHostPool(pool *provider.HostPool) {
	a.hostPool = pool
}

// GetSessionUsage returns current token usage stats
func (a *Assistant) GetSessionUsage() *storage.TokenUsage {
	return a.sessionUsage
}

// GetConversationLength returns the number of messages in the conversation
func (a *Assistant) GetConversationLength() int {
	return len(a.conversation)
}

// AddMCPToolDefinition adds an MCP tool definition for LLM function calling
func (a *Assistant) AddMCPToolDefinition(tool openai.Tool) {
	a.toolDefs = append(a.toolDefs, tool)
}

// RemoveMCPToolDefinitions removes all MCP tool definitions for a server
func (a *Assistant) RemoveMCPToolDefinitions(serverName string) {
	filtered := make([]openai.Tool, 0, len(a.toolDefs))
	prefix := serverName + "."
	for _, tool := range a.toolDefs {
		if tool.Function != nil && !strings.HasPrefix(tool.Function.Name, prefix) {
			filtered = append(filtered, tool)
		}
	}
	a.toolDefs = filtered
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

// GetMode returns the current operating mode
func (a *Assistant) GetMode() storage.OperatingMode {
	if a.mode == "" {
		return storage.ModeDevOps
	}
	return a.mode
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
			fmt.Printf("  Note: Could not unload %s (may not be loaded)\n", oldModel)
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
			fmt.Printf("  Note: Could not save model preference: %v\n", err)
		}
	}

	return nil
}
