package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/app"
	"vclaw/internal/channels/slack"
)

func runSlack(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printSlackUsage()
		return nil
	}
	switch args[0] {
	case "run":
		return runSlackRun(ctx, args[1:])
	case "help", "-h", "--help":
		printSlackUsage()
		return nil
	default:
		return fmt.Errorf("unknown slack command %q", args[0])
	}
}

func runSlackRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vclaw slack run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	botToken := fs.String("bot-token", envFirst("VCLAW_SLACK_BOT_TOKEN", "SLACK_BOT_TOKEN"), "Slack bot token")
	appToken := fs.String("app-token", envFirst("VCLAW_SLACK_APP_TOKEN", "SLACK_APP_TOKEN"), "Slack Socket Mode app token")
	ownerUserID := fs.String("owner-user", firstCSV(envFirst("VCLAW_SLACK_OWNER_USER_ID", "VCLAW_SLACK_ALLOWED_USER_ID", "VCLAW_SLACK_ALLOWED_USER_IDS")), "single owner Slack user ID")
	allowedChannels := fs.String("allowed-channels", envFirst("VCLAW_SLACK_ALLOWED_CHANNEL_IDS"), "optional comma-separated Slack channel IDs")
	dataDir := fs.String("data-dir", envOrDefault("DATA_DIR", "./data"), "runtime data directory")
	maxIterations := fs.Int("max-iterations", agent.DefaultMaxIterations, "maximum agent iterations")
	googleToolsMode := fs.String("google-tools", envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", app.ToolModeAuto), "Google Workspace tool mode: auto, required, or off")
	webToolsMode := fs.String("web-tools", envOrDefault("VCLAW_WEB_TOOLS_MODE", app.ToolModeAuto), "Web search/fetch tool mode: auto, required, or off")
	credentialsPath := fs.String("credentials", envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", defaultCredentialsPath), "Google OAuth desktop client credentials JSON")
	googleTokenPath := fs.String("google-token", envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", defaultTokenPath), "Google OAuth token cache path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*botToken) == "" {
		return fmt.Errorf("VCLAW_SLACK_BOT_TOKEN or SLACK_BOT_TOKEN is required")
	}
	if strings.TrimSpace(*appToken) == "" {
		return fmt.Errorf("VCLAW_SLACK_APP_TOKEN or SLACK_APP_TOKEN is required")
	}
	if strings.TrimSpace(*ownerUserID) == "" {
		return fmt.Errorf("VCLAW_SLACK_OWNER_USER_ID is required")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	bundle, err := app.BuildRuntime(ctx, app.AgentRuntimeConfig{
		DataDir:               *dataDir,
		OpenAIAPIKey:          envFirst("OPENAI_API_KEY", "LLM_API_KEY"),
		OpenAIModel:           envFirst("OPENAI_MODEL", "LLM_MODEL"),
		OpenAIBaseURL:         envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"),
		CompactorModel:        envFirst("VCLAW_COMPACTOR_MODEL"),
		MaxIterations:         *maxIterations,
		GoogleToolsMode:       *googleToolsMode,
		WebToolsMode:          *webToolsMode,
		GoogleCredentialsPath: *credentialsPath,
		GoogleTokenPath:       *googleTokenPath,
		TavilyAPIKey:          envFirst("TAVILY_API_KEY", "TALIVY_API_KEY"),
		TavilyBaseURL:         envFirst("TAVILY_BASE_URL"),
		EnableSandboxTools:    true,
		SandboxWorkspaceDir:   envOrDefault("VCLAW_SANDBOX_WORKSPACE_DIR", ".sandbox-workspace"),
		SandboxImage:          envFirst("VCLAW_SANDBOX_IMAGE"),
		Logger:                logger,
		ParallelExecutionEnabled:   os.Getenv("VCLAW_PARALLEL_ENABLED") == "true",
		ParallelMaxWorkers:         envIntOrDefault("VCLAW_PARALLEL_MAX_WORKERS", 4),
		ParallelToolTimeoutDefault: envDurationOrDefault("VCLAW_PARALLEL_TOOL_TIMEOUT", 30*time.Second),
	})
	if err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting vclaw slack runtime", "model", bundle.Model, "google_tools", *googleToolsMode)
	bot, err := slack.New(slack.Config{
		BotToken:          *botToken,
		AppToken:          *appToken,
		OwnerUserID:       *ownerUserID,
		AllowedChannelIDs: splitCSV(*allowedChannels),
	}, agent.NewRuntimeMessenger(bundle.Runtime), logger)
	if err != nil {
		return err
	}
	if err := bot.Run(runCtx); err != nil && err != context.Canceled {
		return fmt.Errorf("slack bot stopped: %w", err)
	}
	return nil
}

func printSlackUsage() {
	fmt.Println(`Usage:
  vclaw slack run [--google-tools auto|required|off] [--web-tools auto|required|off]

Environment:
  OPENAI_API_KEY                 Required for the real AI provider.
  OPENAI_MODEL                   Optional model override.
  VCLAW_SLACK_BOT_TOKEN          Required unless --bot-token is passed.
  VCLAW_SLACK_APP_TOKEN          Required unless --app-token is passed.
  VCLAW_SLACK_OWNER_USER_ID      Required single-owner Slack user ID.
  VCLAW_SLACK_ALLOWED_CHANNEL_IDS Optional comma-separated channel allow list.`)
}
