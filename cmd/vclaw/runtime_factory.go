package main

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
	googleToolsAuto     = "auto"
	googleToolsRequired = "required"
	googleToolsOff      = "off"
)

type agentRuntimeOptions struct {
	MaxIterations   int
	GoogleToolsMode string
	CredentialsPath string
	GoogleTokenPath string
	Logger          *slog.Logger
}

type agentRuntimeBundle struct {
	Runtime  *agent.Runtime
	Registry *tools.ToolRegistry
	Model    string
}

func newAgentRuntime(ctx context.Context, options agentRuntimeOptions) (agentRuntimeBundle, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return agentRuntimeBundle{}, fmt.Errorf("OPENAI_API_KEY is required")
	}
	model := envOrDefault("OPENAI_MODEL", providers.DefaultOpenAIModel)
	openAI, err := providers.NewOpenAIClient(providers.OpenAIConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: envOrDefault("OPENAI_BASE_URL", ""),
	})
	if err != nil {
		return agentRuntimeBundle{}, err
	}

	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return agentRuntimeBundle{}, err
	}
	if err := sandboxtools.RegisterTools(registry); err != nil {
		return agentRuntimeBundle{}, err
	}
	if err := registerGoogleTools(ctx, registry, options); err != nil {
		return agentRuntimeBundle{}, err
	}

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider:      openAI,
		Registry:      registry,
		SessionStore:  sessions.NewInMemoryStore(),
		MaxIterations: options.MaxIterations,
		Model:         model,
		Logger:        options.Logger,
	})
	return agentRuntimeBundle{Runtime: runtime, Registry: registry, Model: model}, nil
}

func registerGoogleTools(ctx context.Context, registry *tools.ToolRegistry, options agentRuntimeOptions) error {
	mode := strings.ToLower(strings.TrimSpace(options.GoogleToolsMode))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv("VCLAW_GOOGLE_TOOLS_MODE")))
	}
	if mode == "" {
		mode = googleToolsAuto
	}
	if mode == googleToolsOff {
		return nil
	}
	credentialsPath := envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", options.CredentialsPath)
	if strings.TrimSpace(credentialsPath) == "" {
		credentialsPath = defaultCredentialsPath
	}
	tokenPath := envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", options.GoogleTokenPath)
	if strings.TrimSpace(tokenPath) == "" {
		tokenPath = defaultTokenPath
	}
	if mode == googleToolsAuto && (!fileExists(credentialsPath) || !fileExists(tokenPath)) {
		return nil
	}
	if mode != googleToolsAuto && mode != googleToolsRequired {
		return fmt.Errorf("google tools mode must be one of: auto, required, off")
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
	if err := chattools.RegisterTools(registry, chatService); err != nil {
		return err
	}
	return nil
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
