package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/agent"
	"vclaw/internal/connectors/google"
	gcalconnector "vclaw/internal/connectors/google/calendar"
	gchatconnector "vclaw/internal/connectors/google/chat"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gpeopleconnector "vclaw/internal/connectors/google/people"
	"vclaw/internal/connectors/tavily"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/safety"
	sandboxgate "vclaw/internal/sandbox/gate"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	caltools "vclaw/internal/tools/office/calendar"
	chattools "vclaw/internal/tools/office/chat"
	gmailtools "vclaw/internal/tools/office/gmail"
	peopletools "vclaw/internal/tools/office/people"
	sandboxtools "vclaw/internal/tools/system/sandbox"
	webtools "vclaw/internal/tools/web"
)

const (
	googleToolsAuto     = "auto"
	googleToolsRequired = "required"
	googleToolsOff      = "off"

	webToolsAuto     = "auto"
	webToolsRequired = "required"
	webToolsOff      = "off"
)

type agentRuntimeOptions struct {
	MaxIterations   int
	GoogleToolsMode string
	WebToolsMode    string
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
	if err := registerWebTools(registry, options); err != nil {
		return agentRuntimeBundle{}, err
	}
	if err := registerGoogleTools(ctx, registry, options); err != nil {
		return agentRuntimeBundle{}, err
	}

	sessionStore, err := sessions.NewStoreFromEnv()
	if err != nil {
		return agentRuntimeBundle{}, fmt.Errorf("create session store: %w", err)
	}

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider:      openAI,
		Registry:      registry,
		SessionStore:  sessionStore,
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
	dockerRunner := sandboxruntime.NewDockerRunner(sandboxruntime.DockerRunnerConfig{
		Image: image,
		Guard: guard,
	})
	gatedRunner := sandboxgate.NewGatedRunner(sandboxgate.Config{
		Checker:          policies.DefaultChecker,
		Detector:         safety.DefaultScanner,
		Runner:           dockerRunner,
		SkipApprovalGate: true, // agent ToolPolicy HITL handles approval; gate enforces block only
	})
	return sandboxtools.Config{
		Runner:              gatedRunner,
		Guard:               guard,
		DefaultWorkspaceDir: guard.Root(),
	}, nil
}

func registerWebTools(registry *tools.ToolRegistry, options agentRuntimeOptions) error {
	mode := strings.ToLower(strings.TrimSpace(options.WebToolsMode))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv("VCLAW_WEB_TOOLS_MODE")))
	}
	if mode == "" {
		mode = webToolsAuto
	}
	if mode == webToolsOff {
		return nil
	}
	if mode != webToolsAuto && mode != webToolsRequired {
		return fmt.Errorf("web tools mode must be one of: auto, required, off")
	}

	apiKey := envFirst("TAVILY_API_KEY", "TALIVY_API_KEY")
	if apiKey == "" {
		if mode == webToolsAuto {
			return nil
		}
		return fmt.Errorf("TAVILY_API_KEY is required when web tools mode is required")
	}
	client, err := tavily.NewClient(tavily.Config{
		APIKey:  apiKey,
		BaseURL: envOrDefault("TAVILY_BASE_URL", ""),
	})
	if err != nil {
		return fmt.Errorf("configure web tools: %w", err)
	}
	return webtools.RegisterTools(registry, webtools.NewService(client))
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
