package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/agent"
	"vclaw/internal/agent/reference"
	"vclaw/internal/connectors/google"
	gcal "vclaw/internal/connectors/google/calendar"
	gchat "vclaw/internal/connectors/google/chat"
	gdocs "vclaw/internal/connectors/google/docs"
	gdrive "vclaw/internal/connectors/google/drive"
	ggmail "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gpeople "vclaw/internal/connectors/google/people"
	gsheets "vclaw/internal/connectors/google/sheets"
	gpeople "vclaw/internal/connectors/google/people"
	"vclaw/internal/connectors/tavily"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/safety"
	sandboxgate "vclaw/internal/sandbox/gate"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	docstool "vclaw/internal/tools/office/docs"
	drivetool "vclaw/internal/tools/office/drive"
	gmailtool "vclaw/internal/tools/office/gmail"
	peopletool "vclaw/internal/tools/office/people"
	sheetstool "vclaw/internal/tools/office/sheets"
	fstool "vclaw/internal/tools/os/filesystem"
	peopletool "vclaw/internal/tools/office/people"
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

	Provider     providers.Provider
	SessionStore sessions.Store
	StateStore   agent.RuntimeStateStore

	Logger        *slog.Logger
	MaxIterations int

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
}

type RuntimeBundle struct {
	Runtime  *agent.Runtime
	Registry *tools.ToolRegistry
	Model    string
}

func BuildRuntime(ctx context.Context, config AgentRuntimeConfig) (RuntimeBundle, error) {
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

	registry, err := NewAgentToolRegistry(ctx, config)
	if err != nil {
		return RuntimeBundle{}, err
	}

	dataDir := strings.TrimSpace(config.DataDir)
	if dataDir == "" {
		dataDir = "./data"
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

	compactorModel := strings.TrimSpace(config.CompactorModel)
	if compactorModel == "" {
		compactorModel = model
	}
	compactor := sessions.NewCompactor(provider, sessions.CompactorConfig{
		SummarizeModel: compactorModel,
	}, config.Logger)

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider: provider,
		Registry: registry,
		ReferenceResolver: reference.NewFallbackResolver(
			reference.NewLLMResolver(provider, model),
			reference.NewHeuristicResolver(),
		),
		SessionStore:          sessionStore,
		StateStore:            stateStore,
		Logger:                config.Logger,
		MaxIterations:         config.MaxIterations,
		Model:                 model,
		Compactor:             compactor,
		MemoryClassifierModel: compactorModel,
	})
	return RuntimeBundle{Runtime: runtime, Registry: registry, Model: model}, nil
}

func NewAgentToolRegistry(ctx context.Context, config AgentRuntimeConfig) (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return nil, err
	}
	fstoolConfig := fstool.Config{
		AllowedRoots: []string{strings.TrimSpace(config.SandboxWorkspaceDir)},
	}
	if err := fstool.RegisterTools(registry, fstoolConfig); err != nil {
		return nil, fmt.Errorf("register filesystem tools: %w", err)
	}
	if config.EnableSandboxTools {
		sandboxConfig, err := newSandboxToolConfig(config)
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
	calendarClient, err := gcal.NewClient(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("create calendar connector: %w", err)
	}
	if err := calendartool.RegisterTools(registry, calendartool.NewService(calendarClient)); err != nil {
		return err
	}

	chatService := chattool.NewService(gchat.NewClient(httpClient))
	if err := chattool.RegisterTools(registry, chatService); err != nil {
		return nil, fmt.Errorf("register chat tools: %w", err)
	}

	gmailService := gmailtool.NewService(ggmail.NewClient(httpClient))
	if err := gmailtool.RegisterTools(registry, gmailService); err != nil {
		return nil, fmt.Errorf("register gmail tools: %w", err)
	}

	peopleService := peopletool.NewService(gpeople.NewClient(httpClient))
	if err := peopletool.RegisterTools(registry, peopleService); err != nil {
		return nil, fmt.Errorf("register people tools: %w", err)
	}

	driveClient, err := gdrive.NewClient(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("drive client: %w", err)
	}
	if err := drivetool.RegisterTools(registry, drivetool.NewService(driveClient)); err != nil {
		return nil, fmt.Errorf("register drive tools: %w", err)
	}

	docsClient, err := gdocs.NewClient(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("docs client: %w", err)
	}
	if err := docstool.RegisterTools(registry, docstool.NewService(docsClient)); err != nil {
		return nil, fmt.Errorf("register docs tools: %w", err)
	}

	sheetsClient, err := gsheets.NewClient(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("sheets client: %w", err)
	}
	if err := sheetstool.RegisterTools(registry, sheetstool.NewService(sheetsClient)); err != nil {
		return nil, fmt.Errorf("register sheets tools: %w", err)
	}

	return registry, nil
	peopleClient := gpeople.NewClient(httpClient)
	if err := chattool.RegisterTools(registry, chattool.NewServiceWithPeople(gchat.NewClient(httpClient), peopleClient)); err != nil {
		return err
	}
	return peopletool.RegisterTools(registry, peopletool.NewService(peopleClient))
}

func newSandboxToolConfig(config AgentRuntimeConfig) (sandboxtool.Config, error) {
	if config.SandboxRunner != nil {
		return sandboxtool.Config{
			Runner:              config.SandboxRunner,
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
	gatedRunner := sandboxgate.NewGatedRunner(sandboxgate.Config{
		Checker:          policies.DefaultChecker,
		Detector:         safety.DefaultScanner,
		Runner:           dockerRunner,
		SkipApprovalGate: true,
	})
	return sandboxtool.Config{
		Runner:              gatedRunner,
		Guard:               guard,
		DefaultWorkspaceDir: guard.Root(),
	}, nil
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
