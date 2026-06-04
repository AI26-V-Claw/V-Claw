package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/agent"
	agentintent "vclaw/internal/agent/intent"
	"vclaw/internal/connectors/google"
	gcal "vclaw/internal/connectors/google/calendar"
	gchat "vclaw/internal/connectors/google/chat"
	ggmail "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	"vclaw/internal/providers"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	gmailtool "vclaw/internal/tools/office/gmail"
	sandboxtool "vclaw/internal/tools/system/sandbox"
)

type AgentRuntimeConfig struct {
	OpenAIAPIKey          string
	OpenAIModel           string
	OpenAIBaseURL         string
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

	intentClassifier, err := NewIntentClassifier(provider)
	if err != nil {
		return nil, fmt.Errorf("create intent classifier: %w", err)
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

	return agent.NewRuntime(agent.RuntimeConfig{
		Provider:         provider,
		Registry:         registry,
		IntentClassifier: intentClassifier,
		TaskPlanner:      agent.NewLLMTaskPlanner(provider, model),
		SessionStore:     sessionStore,
		Logger:           config.Logger,
		MaxIterations:    config.MaxIterations,
		Model:            model,
	}), nil
}

func NewIntentClassifier(provider providers.Provider) (agent.IntentClassifier, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VCLAW_INTENT_CLASSIFIER_MODE")))
	if mode == "" {
		mode = agentintent.ClassifierModeFallback
	}
	heuristic := agentintent.NewHeuristicRunner(agentintent.DefaultConfig)
	switch mode {
	case agentintent.ClassifierModeHeuristic:
		return heuristic, nil
	case agentintent.ClassifierModeLLM:
		return agentintent.NewLLMClassifier(provider, agentintent.DefaultConfig)
	case agentintent.ClassifierModeFallback:
		llm, err := agentintent.NewLLMClassifier(provider, agentintent.DefaultConfig)
		if err != nil {
			return nil, err
		}
		return agentintent.NewFallbackClassifier(llm, heuristic), nil
	default:
		return nil, fmt.Errorf("intent classifier mode must be one of: fallback, llm, heuristic")
	}
}

func NewAgentToolRegistry(ctx context.Context, config AgentRuntimeConfig) (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return nil, err
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
	runner := sandboxruntime.NewDockerRunner(sandboxruntime.DockerRunnerConfig{
		Image: strings.TrimSpace(config.SandboxImage),
		Guard: guard,
	})
	return sandboxtool.Config{
		Runner:              runner,
		DefaultWorkspaceDir: guard.Root(),
	}, nil
}
