package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"vclaw/internal/agent"
	"vclaw/internal/connectors/google"
	gcal "vclaw/internal/connectors/google/calendar"
	gchat "vclaw/internal/connectors/google/chat"
	ggmail "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gpeople "vclaw/internal/connectors/google/people"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	gmailtool "vclaw/internal/tools/office/gmail"
	peopletool "vclaw/internal/tools/office/people"
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

	return agent.NewRuntime(agent.RuntimeConfig{
		Provider:      provider,
		Registry:      registry,
		SessionStore:  config.SessionStore,
		Logger:        config.Logger,
		MaxIterations: config.MaxIterations,
		Model:         model,
	}), nil
}

func NewAgentToolRegistry(ctx context.Context, config AgentRuntimeConfig) (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return nil, err
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

	peopleService := peopletool.NewService(gpeople.NewClient(httpClient))
	if err := peopletool.RegisterTools(registry, peopleService); err != nil {
		return nil, fmt.Errorf("register people tools: %w", err)
	}

	return registry, nil
}
