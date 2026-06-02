package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"vclaw/internal/agent"
	"vclaw/internal/connectors/google"
	gcalconnector "vclaw/internal/connectors/google/calendar"
	gchatconnector "vclaw/internal/connectors/google/chat"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	caltools "vclaw/internal/tools/office/calendar"
	chattools "vclaw/internal/tools/office/chat"
	gmailtools "vclaw/internal/tools/office/gmail"
	sandboxtools "vclaw/internal/tools/system/sandbox"
)

const (
	defaultCredentialsPath = "configs/google/credentials.json"
	defaultTokenPath       = "configs/google/token.json"
)

type AgentRuntimeConfig struct {
	OpenAIAPIKey          string
	OpenAIModel           string
	OpenAIBaseURL         string
	Logger                *slog.Logger
	EnableGoogleTools     bool
	GoogleCredentialsPath string
	GoogleTokenPath       string
}

func NewAgentRuntime(ctx context.Context, config AgentRuntimeConfig) (*agent.Runtime, error) {
	apiKey := strings.TrimSpace(config.OpenAIAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	model := strings.TrimSpace(config.OpenAIModel)
	if model == "" {
		model = providers.DefaultOpenAIModel
	}

	provider, err := providers.NewOpenAIClient(providers.OpenAIConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: config.OpenAIBaseURL,
	})
	if err != nil {
		return nil, err
	}

	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return nil, err
	}
	if err := sandboxtools.RegisterTools(registry); err != nil {
		return nil, err
	}
	if config.EnableGoogleTools {
		if err := registerGoogleTools(ctx, registry, config); err != nil {
			return nil, err
		}
	}

	return agent.NewRuntime(agent.RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: sessions.NewInMemoryStore(),
		Model:        model,
		Logger:       config.Logger,
	}), nil
}

func registerGoogleTools(ctx context.Context, registry *tools.ToolRegistry, config AgentRuntimeConfig) error {
	credentialsPath := strings.TrimSpace(config.GoogleCredentialsPath)
	if credentialsPath == "" {
		credentialsPath = defaultCredentialsPath
	}
	tokenPath := strings.TrimSpace(config.GoogleTokenPath)
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}
	if !fileExists(credentialsPath) || !fileExists(tokenPath) {
		return fmt.Errorf("google tools enabled but credentials/token files are missing")
	}

	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return fmt.Errorf("configure Google tools: %w", err)
	}

	gmailService := gmailtools.NewService(gmailconnector.NewClient(httpClient))
	if err := gmailtools.RegisterTools(registry, gmailService); err != nil {
		return err
	}

	calendarClient, err := gcalconnector.NewClient(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("create calendar connector: %w", err)
	}
	if err := caltools.RegisterTools(registry, caltools.NewService(calendarClient)); err != nil {
		return err
	}

	chatService := chattools.NewService(gchatconnector.NewClient(httpClient))
	return chattools.RegisterTools(registry, chatService)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
