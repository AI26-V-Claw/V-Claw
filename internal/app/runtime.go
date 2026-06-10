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
	ggmail "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/safety"
	sandboxgate "vclaw/internal/sandbox/gate"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	gmailtool "vclaw/internal/tools/office/gmail"
	fstool "vclaw/internal/tools/os/filesystem"
	sandboxtool "vclaw/internal/tools/system/sandbox"
)

type AgentRuntimeConfig struct {
	OpenAIAPIKey          string
	OpenAIModel           string
	OpenAIBaseURL         string
	// CompactorModel is the LLM model used for session summarization.
	// Should be a cheaper model than OpenAIModel (e.g. "gpt-4o-mini").
	// Defaults to OpenAIModel when empty.
	CompactorModel        string
	Provider              providers.Provider
	SessionStore          sessions.Store
	Logger                *slog.Logger
	MaxIterations         int
	EnableGoogleTools     bool
	GoogleCredentialsPath string
	GoogleTokenPath       string
	EnableSandboxTools    bool
	SandboxWorkspaceDir   string
	SandboxImage          string
	SandboxRunner         sandboxruntime.Runner
}

func NewAgentRuntime(ctx context.Context, config AgentRuntimeConfig) (*agent.Runtime, error) {
	provider := config.Provider
	if provider == nil {
		apiKey := strings.TrimSpace(config.OpenAIAPIKey)
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is required")
		}
		openAI, err := providers.NewOpenAIClient(providers.OpenAIConfig{
			APIKey:  apiKey,
			Model:   config.OpenAIModel,
			BaseURL: config.OpenAIBaseURL,
		})
		if err != nil {
			return nil, err
		}
		provider = openAI
	}

	registry, err := NewAgentToolRegistry(ctx, config)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(config.OpenAIModel)
	if model == "" {
		model = providers.DefaultOpenAIModel
	}
	sessionStore := config.SessionStore
	if sessionStore == nil {
		sessionStore, err = sessions.NewStoreFromEnv()
		if err != nil {
			return nil, fmt.Errorf("create session store: %w", err)
		}
	}

	compactorModel := strings.TrimSpace(config.CompactorModel)
	if compactorModel == "" {
		compactorModel = model
	}
	compactor := sessions.NewCompactor(provider, sessions.CompactorConfig{
		SummarizeModel: compactorModel,
	}, config.Logger)

	return agent.NewRuntime(agent.RuntimeConfig{
		Provider: provider,
		Registry: registry,
		ReferenceResolver: reference.NewFallbackResolver(
			reference.NewLLMResolver(provider, model),
			reference.NewHeuristicResolver(),
		),
		SessionStore:          sessionStore,
		Logger:                config.Logger,
		MaxIterations:         config.MaxIterations,
		Model:                 model,
		Compactor:             compactor,
		MemoryClassifierModel: compactorModel,
	}), nil
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
	if !config.EnableGoogleTools {
		return registry, nil
	}

	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: config.GoogleCredentialsPath,
		TokenPath:       config.GoogleTokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return nil, fmt.Errorf("google oauth: %w", err)
	}

	calendarClient, err := gcal.NewClient(ctx, httpClient)
	if err != nil {
		return nil, fmt.Errorf("calendar client: %w", err)
	}
	if err := calendartool.RegisterTools(registry, calendartool.NewService(calendarClient)); err != nil {
		return nil, fmt.Errorf("register calendar tools: %w", err)
	}

	chatService := chattool.NewService(gchat.NewClient(httpClient))
	if err := chattool.RegisterTools(registry, chatService); err != nil {
		return nil, fmt.Errorf("register chat tools: %w", err)
	}

	gmailService := gmailtool.NewService(ggmail.NewClient(httpClient))
	if err := gmailtool.RegisterTools(registry, gmailService); err != nil {
		return nil, fmt.Errorf("register gmail tools: %w", err)
	}

	return registry, nil
}

func newSandboxToolConfig(config AgentRuntimeConfig) (sandboxtool.Config, error) {
	if config.SandboxRunner != nil {
		workspaceDir := strings.TrimSpace(config.SandboxWorkspaceDir)
		return sandboxtool.Config{
			Runner:              config.SandboxRunner,
			DefaultWorkspaceDir: workspaceDir,
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
		SkipApprovalGate: true, // agent ToolPolicy HITL handles approval; gate enforces block only
	})
	return sandboxtool.Config{
		Runner:              gatedRunner,
		Guard:               guard,
		DefaultWorkspaceDir: guard.Root(),
	}, nil
}
