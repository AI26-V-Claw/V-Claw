package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/agent"
	"vclaw/internal/audit"
	"vclaw/internal/connectors/google"
	gcalconnector "vclaw/internal/connectors/google/calendar"
	gchatconnector "vclaw/internal/connectors/google/chat"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gpeopleconnector "vclaw/internal/connectors/google/people"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/safety"
	"vclaw/internal/sandbox/gate"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	caltools "vclaw/internal/tools/office/calendar"
	chattools "vclaw/internal/tools/office/chat"
	gmailtools "vclaw/internal/tools/office/gmail"
	peopletools "vclaw/internal/tools/office/people"
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
	sandboxConfig, err := newSandboxToolConfig()
	if err != nil {
		return agentRuntimeBundle{}, err
	}
	if err := sandboxtools.RegisterToolsWithConfig(registry, sandboxConfig); err != nil {
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

func newSandboxToolConfig() (sandboxtools.Config, error) {
	workspaceDir := strings.TrimSpace(os.Getenv("VCLAW_SANDBOX_WORKSPACE_DIR"))
	if workspaceDir == "" {
		workspaceDir = ".sandbox-workspace"
	}
	if !filepath.IsAbs(workspaceDir) {
		abs, err := filepath.Abs(workspaceDir)
		if err != nil {
			return sandboxtools.Config{}, fmt.Errorf("resolve sandbox workspace: %w", err)
		}
		workspaceDir = abs
	}
	if err := os.MkdirAll(workspaceDir, 0750); err != nil {
		return sandboxtools.Config{}, fmt.Errorf("create sandbox workspace: %w", err)
	}

	guard, err := sandboxruntime.NewWorkspaceGuard(workspaceDir)
	if err != nil {
		return sandboxtools.Config{}, err
	}

	image := strings.TrimSpace(os.Getenv("VCLAW_SANDBOX_IMAGE"))
	runner := sandboxruntime.NewDockerRunner(sandboxruntime.DockerRunnerConfig{
		Image: image,
		Guard: guard,
	})
	gated := gate.NewGatedRunner(gate.Config{
		Checker:  policies.DefaultChecker,
		Detector: safety.DefaultScanner,
		Logger:   &audit.NopLogger{},
		Runner:   runner,
	})
	return sandboxtools.Config{
		Runner:              gated,
		DefaultWorkspaceDir: guard.Root(),
	}, nil
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

	peopleService := peopletools.NewService(gpeopleconnector.NewClient(httpClient))
	if err := peopletools.RegisterTools(registry, peopleService); err != nil {
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
