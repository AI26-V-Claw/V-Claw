package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/agent/reference"
	"vclaw/internal/audit"
	"vclaw/internal/connectors/google"
	gcal "vclaw/internal/connectors/google/calendar"
	gchat "vclaw/internal/connectors/google/chat"
	gdocs "vclaw/internal/connectors/google/docs"
	gdrive "vclaw/internal/connectors/google/drive"
	ggmail "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gpeople "vclaw/internal/connectors/google/people"
	gsheets "vclaw/internal/connectors/google/sheets"
	"vclaw/internal/connectors/tavily"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/safety"
	sandboxgate "vclaw/internal/sandbox/gate"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/sessions"
	"vclaw/internal/toolhooks"
	pgstore "vclaw/internal/store/pg"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	docstool "vclaw/internal/tools/office/docs"
	drivetool "vclaw/internal/tools/office/drive"
	gmailtool "vclaw/internal/tools/office/gmail"
	peopletool "vclaw/internal/tools/office/people"
	sheetstool "vclaw/internal/tools/office/sheets"
	fstool "vclaw/internal/tools/os/filesystem"
	sandboxtool "vclaw/internal/tools/system/sandbox"
	webtool "vclaw/internal/tools/web"
)

const (
	ToolModeAuto     = "auto"
	ToolModeRequired = "required"
	ToolModeOff      = "off"
)

type AgentRuntimeConfig struct {
	DataDir        string
	OpenAIAPIKey   string
	OpenAIModel    string
	OpenAIBaseURL  string
	CompactorModel string
	LongMemDir     string // path to cache/memory/; defaults to ./cache/memory

	Provider     providers.Provider
	SessionStore sessions.Store
	StateStore   agent.RuntimeStateStore
	AuditLogger  audit.AuditEventLogger
	DatabaseURL  string

	Logger        *slog.Logger
	ToolHooks     toolhooks.Hooks
	MaxIterations int
	Observer      agent.RuntimeObserver

	GoogleToolsMode       string
	GoogleCredentialsPath string
	GoogleTokenPath       string

	WebToolsMode  string
	TavilyAPIKey  string
	TavilyBaseURL string

	EnableSandboxTools  bool
	SandboxWorkspaceDir string
	SandboxImage        string
	SandboxRunner       sandboxruntime.Runner

	ParallelExecutionEnabled   bool
	ParallelMaxWorkers         int
	ParallelToolTimeoutDefault time.Duration
}

type RuntimeBundle struct {
	Runtime               *agent.Runtime
	Registry              *tools.ToolRegistry
	Model                 string
	PolicyStore           *policies.UserPolicyStore
	Provider              providers.Provider
	GoogleOAuthConfigured bool
	TavilyConfigured      bool
}

type toolRegistryPersister interface {
	UpsertToolRegistryEntries(context.Context, []tools.ToolDefinition) error
}

func BuildRuntime(ctx context.Context, config AgentRuntimeConfig) (RuntimeBundle, error) {
	dataDir := strings.TrimSpace(config.DataDir)
	if dataDir == "" {
		dataDir = "./data"
	}
	provider := config.Provider
	model := strings.TrimSpace(config.OpenAIModel)
	if model == "" {
		model = providers.DefaultOpenAIModel
	}
	if provider == nil {
		apiKey := strings.TrimSpace(config.OpenAIAPIKey)
		if apiKey == "" {
			return RuntimeBundle{}, fmt.Errorf("OPENAI_API_KEY is required")
		}
		openAI, err := providers.NewOpenAIClient(providers.OpenAIConfig{
			APIKey:  apiKey,
			Model:   model,
			BaseURL: config.OpenAIBaseURL,
		})
		if err != nil {
			return RuntimeBundle{}, err
		}
		provider = openAI
	}

	databaseURL := strings.TrimSpace(config.DatabaseURL)
	if databaseURL != "" && (config.StateStore == nil || config.AuditLogger == nil) {
		store, err := pgstore.New(ctx, databaseURL)
		if err != nil {
			return RuntimeBundle{}, fmt.Errorf("connect database store: %w", err)
		}
		if config.StateStore == nil {
			config.StateStore = store
		}
		if config.AuditLogger == nil {
			config.AuditLogger = store
		}
	}
	auditLogger, err := resolveAuditLogger(dataDir, config.AuditLogger)
	if err != nil {
		return RuntimeBundle{}, fmt.Errorf("create audit logger: %w", err)
	}
	config.AuditLogger = auditLogger
	auditHooks := toolhooks.AuditHooks{Logger: auditLogger}
	registry, err := NewAgentToolRegistry(ctx, config)
	if err != nil {
		return RuntimeBundle{}, err
	}
	if err := persistToolRegistry(ctx, registry, config.SessionStore, config.StateStore, config.AuditLogger); err != nil {
		return RuntimeBundle{}, err
	}

	sessionStore := config.SessionStore
	if sessionStore == nil {
		sessionStore, err = sessions.NewFileStore(dataDir)
		if err != nil {
			return RuntimeBundle{}, fmt.Errorf("create session store: %w", err)
		}
	}
	stateStore := config.StateStore
	if stateStore == nil {
		stateStore, err = agent.NewFileRuntimeStateStore(dataDir)
		if err != nil {
			return RuntimeBundle{}, fmt.Errorf("create runtime state store: %w", err)
		}
	}
	userPolicy, policyStore, err := loadToolPolicy(config.Logger)
	if err != nil {
		return RuntimeBundle{}, fmt.Errorf("load user policy config: %w", err)
	}

	compactorModel := strings.TrimSpace(config.CompactorModel)
	if compactorModel == "" {
		compactorModel = model
	}
	compactor := sessions.NewCompactor(provider, sessions.CompactorConfig{
		SummarizeModel: compactorModel,
	}, config.Logger)
	longMemDir := strings.TrimSpace(config.LongMemDir)
	if longMemDir == "" {
		longMemDir = "./cache/memory"
	}
	if err := os.MkdirAll(longMemDir, 0700); err != nil {
		return RuntimeBundle{}, fmt.Errorf("create long-term memory dir: %w", err)
	}
	var runtimeHooks toolhooks.Hooks = auditHooks
	if config.ToolHooks != nil {
		runtimeHooks = toolhooks.ChainHooks{config.ToolHooks, auditHooks}
	}

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider: provider,
		Registry: registry,
		Observer: config.Observer,
		ReferenceResolver: reference.NewFallbackResolver(
			reference.NewLLMResolver(provider, model),
			reference.NewHeuristicResolver(),
		),
		SessionStore:               sessionStore,
		Policy:                     userPolicy,
		StateStore:                 stateStore,
		Logger:                     config.Logger,
		ToolHooks:                  runtimeHooks,
		MaxIterations:              config.MaxIterations,
		Model:                      model,
		Compactor:                  compactor,
		MemoryClassifierModel:      compactorModel,
		LongMemDir:                 longMemDir,
		ParallelExecutionEnabled:   config.ParallelExecutionEnabled,
		ParallelMaxWorkers:         config.ParallelMaxWorkers,
		ParallelToolTimeoutDefault: config.ParallelToolTimeoutDefault,
	})
	return RuntimeBundle{
		Runtime:               runtime,
		Registry:              registry,
		Model:                 model,
		PolicyStore:           policyStore,
		Provider:              provider,
		GoogleOAuthConfigured: googleOAuthConfigured(config),
		TavilyConfigured:      strings.TrimSpace(config.TavilyAPIKey) != "",
	}, nil
}

func persistToolRegistry(ctx context.Context, registry *tools.ToolRegistry, stores ...any) error {
	for _, store := range stores {
		persister, ok := store.(toolRegistryPersister)
		if !ok || persister == nil {
			continue
		}
		if err := persister.UpsertToolRegistryEntries(ctx, registry.ListTools()); err != nil {
			return fmt.Errorf("persist tool registry: %w", err)
		}
	}
	return nil
}

func NewAgentToolRegistry(ctx context.Context, config AgentRuntimeConfig) (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return nil, err
	}
	// filesystem tools must use the same directory that sandbox.runShell mounts as /workspace.
	// sandbox.runShell calls PrepareSessionWorkspace(DefaultSessionID) → <root>/<session>/workspace/.
	// Aligning AllowedRoots here ensures writeFile/readFile/deleteFile operate on the same path.
	fsRoot := sandboxWorkspaceFSRoot(config)
	fstoolConfig := fstool.Config{
		AllowedRoots: []string{fsRoot},
	}
	if err := fstool.RegisterTools(registry, fstoolConfig); err != nil {
		return nil, fmt.Errorf("register filesystem tools: %w", err)
	}
	if config.EnableSandboxTools {
		sandboxConfig, err := newSandboxToolConfig(config, config.ToolHooks)
		if err != nil {
			return nil, fmt.Errorf("configure sandbox tools: %w", err)
		}
		if err := sandboxtool.RegisterToolsWithConfig(registry, sandboxConfig); err != nil {
			return nil, fmt.Errorf("register sandbox tools: %w", err)
		}
	}
	if err := registerWebTools(registry, config); err != nil {
		return nil, err
	}
	if err := registerGoogleTools(ctx, registry, config); err != nil {
		return nil, err
	}
	return registry, nil
}

func registerWebTools(registry *tools.ToolRegistry, config AgentRuntimeConfig) error {
	mode, err := normalizeToolMode(config.WebToolsMode)
	if err != nil {
		return fmt.Errorf("web tools mode: %w", err)
	}
	if mode == ToolModeOff {
		return nil
	}
	apiKey := strings.TrimSpace(config.TavilyAPIKey)
	if apiKey == "" {
		if mode == ToolModeAuto {
			return nil
		}
		return fmt.Errorf("TAVILY_API_KEY is required when web tools mode is required")
	}
	client, err := tavily.NewClient(tavily.Config{
		APIKey:  apiKey,
		BaseURL: strings.TrimSpace(config.TavilyBaseURL),
	})
	if err != nil {
		return fmt.Errorf("configure web tools: %w", err)
	}
	return webtool.RegisterTools(registry, webtool.NewService(client))
}

func registerGoogleTools(ctx context.Context, registry *tools.ToolRegistry, config AgentRuntimeConfig) error {
	mode, err := normalizeToolMode(config.GoogleToolsMode)
	if err != nil {
		return fmt.Errorf("google tools mode: %w", err)
	}
	if mode == ToolModeOff {
		return nil
	}
	credentialsPath := strings.TrimSpace(config.GoogleCredentialsPath)
	tokenPath := strings.TrimSpace(config.GoogleTokenPath)
	if mode == ToolModeAuto && (!fileExists(credentialsPath) || !fileExists(tokenPath)) {
		return nil
	}

	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return fmt.Errorf("configure Google tools: %w", err)
	}

	if err := gmailtool.RegisterTools(registry, gmailtool.NewService(ggmail.NewClient(httpClient))); err != nil {
		return err
	}
	// drive.uploadFile may only read local files from the same sandbox workspace
	// the filesystem tools are restricted to — never arbitrary host paths.
	driveUploadGuard := fstool.NewPathGuard([]string{sandboxWorkspaceFSRoot(config)})
	if err := drivetool.RegisterTools(registry, drivetool.NewService(gdrive.NewClient(httpClient)), driveUploadGuard); err != nil {
		return err
	}
	if err := docstool.RegisterTools(registry, docstool.NewService(gdocs.NewClient(httpClient))); err != nil {
		return err
	}
	if err := sheetstool.RegisterTools(registry, sheetstool.NewService(gsheets.NewClient(httpClient))); err != nil {
		return err
	}
	calendarClient, err := gcal.NewClient(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("create calendar connector: %w", err)
	}
	if err := calendartool.RegisterTools(registry, calendartool.NewService(calendarClient)); err != nil {
		return err
	}
	peopleClient := gpeople.NewClient(httpClient)
	if err := chattool.RegisterTools(registry, chattool.NewServiceWithPeople(gchat.NewClient(httpClient), peopleClient)); err != nil {
		return err
	}
	return peopletool.RegisterTools(registry, peopletool.NewService(peopleClient))
}

// sandboxWorkspaceFSRoot returns the single workspace directory that both the
// filesystem tools and drive.uploadFile are restricted to. It must match what
// sandbox.runShell mounts as /workspace so all local file access stays aligned.
func sandboxWorkspaceFSRoot(config AgentRuntimeConfig) string {
	sandboxRoot := strings.TrimSpace(config.SandboxWorkspaceDir)
	if sandboxRoot == "" {
		sandboxRoot = ".sandbox-workspace"
	}
	return filepath.Join(sandboxRoot, sandboxtool.DefaultSessionID, "workspace")
}

func newSandboxToolConfig(config AgentRuntimeConfig, hooks toolhooks.Hooks) (sandboxtool.Config, error) {
	if config.SandboxRunner != nil {
		return sandboxtool.Config{
			Runner:              newSandboxGate(config, hooks, config.SandboxRunner),
			DefaultWorkspaceDir: strings.TrimSpace(config.SandboxWorkspaceDir),
		}, nil
	}

	workspaceDir := strings.TrimSpace(config.SandboxWorkspaceDir)
	if workspaceDir == "" {
		workspaceDir = ".sandbox-workspace"
	}
	if !filepath.IsAbs(workspaceDir) {
		abs, err := filepath.Abs(workspaceDir)
		if err != nil {
			return sandboxtool.Config{}, fmt.Errorf("resolve sandbox workspace: %w", err)
		}
		workspaceDir = abs
	}
	if err := os.MkdirAll(workspaceDir, 0750); err != nil {
		return sandboxtool.Config{}, fmt.Errorf("create sandbox workspace: %w", err)
	}

	guard, err := sandboxruntime.NewWorkspaceGuard(workspaceDir)
	if err != nil {
		return sandboxtool.Config{}, err
	}
	dockerRunner := sandboxruntime.NewDockerRunner(sandboxruntime.DockerRunnerConfig{
		Image: strings.TrimSpace(config.SandboxImage),
		Guard: guard,
	})
	return sandboxtool.Config{
		Runner:              newSandboxGate(config, hooks, dockerRunner),
		Guard:               guard,
		DefaultWorkspaceDir: guard.Root(),
	}, nil
}

func newSandboxGate(config AgentRuntimeConfig, hooks toolhooks.Hooks, runner sandboxruntime.Runner) sandboxruntime.Runner {
	baseHooks := toolhooks.SandboxPolicyHooks{
		Checker:          policies.DefaultChecker,
		Detector:         safety.DefaultScanner,
		Logger:           config.AuditLogger,
		SkipApprovalGate: true,
	}
	var sandboxHooks toolhooks.Hooks = baseHooks
	if hooks != nil {
		sandboxHooks = toolhooks.ChainHooks{hooks, baseHooks}
	}
	for {
		gated, ok := runner.(*sandboxgate.GatedRunner)
		if !ok {
			break
		}
		existingHooks := gated.Hooks()
		runner = gated.Runner()
		if existingHooks != nil {
			sandboxHooks = toolhooks.ChainHooks{existingHooks, sandboxHooks}
		}
	}
	return sandboxgate.NewGatedRunner(sandboxgate.Config{
		ToolHooks: sandboxHooks,
		Runner:    runner,
	})
}

func resolveAuditLogger(dataDir string, provided audit.AuditEventLogger) (audit.AuditEventLogger, error) {
	if provided != nil {
		return provided, nil
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "."
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	return audit.NewFileLogger(filepath.Join(dataDir, "tool_audit.jsonl"))
}

func loadToolPolicy(logger *slog.Logger) (policies.ToolPolicy, *policies.UserPolicyStore, error) {
	dataDir := envOrDefault("DATA_DIR", "./data")
	path := envOrDefault("VCLAW_USER_POLICY_PATH", policies.DefaultUserPolicyPath(dataDir))
	missing := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		missing = true
	}
	store, err := policies.NewUserPolicyStore(path)
	if err != nil {
		return policies.ToolPolicy{}, nil, err
	}
	cfg := store.Snapshot()
	if logger == nil {
		logger = slog.Default()
	}
	if missing && len(cfg.AutoAllow) == 0 && len(cfg.RequireApproval) == 0 && len(cfg.AlwaysBlock) == 0 {
		logger.Warn("user policy config missing; using empty policy defaults",
			"path", path,
			"auto_allow", cfg.AutoAllow,
			"require_approval", cfg.RequireApproval,
			"always_block", cfg.AlwaysBlock,
		)
	} else {
		logger.Info("loaded user policy config",
			"path", path,
			"auto_allow", cfg.AutoAllow,
			"require_approval", cfg.RequireApproval,
			"always_block", cfg.AlwaysBlock,
		)
	}
	return policies.NewToolPolicyWithStore(store), store, nil
}

func googleOAuthConfigured(config AgentRuntimeConfig) bool {
	mode, err := normalizeToolMode(config.GoogleToolsMode)
	if err != nil || mode == ToolModeOff {
		return false
	}
	credentialsPath := strings.TrimSpace(config.GoogleCredentialsPath)
	tokenPath := strings.TrimSpace(config.GoogleTokenPath)
	if mode == ToolModeRequired {
		return true
	}
	return fileExists(credentialsPath) && fileExists(tokenPath)
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeToolMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return ToolModeOff, nil
	}
	switch mode {
	case ToolModeAuto, ToolModeRequired, ToolModeOff:
		return mode, nil
	default:
		return "", fmt.Errorf("must be one of: auto, required, off")
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
