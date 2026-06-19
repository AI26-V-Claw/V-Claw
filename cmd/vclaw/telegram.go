package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/app"
	"vclaw/internal/channels/telegram"
	"vclaw/internal/monitoring"
)

func runTelegram(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printTelegramUsage()
		return nil
	}
	switch args[0] {
	case "run":
		return runTelegramRun(ctx, args[1:])
	case "help", "-h", "--help":
		printTelegramUsage()
		return nil
	default:
		return fmt.Errorf("unknown telegram command %q", args[0])
	}
}

func runTelegramRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vclaw telegram run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	botToken := fs.String("token", envFirst("TELEGRAM_BOT_TOKEN", "VCLAW_TELEGRAM_BOT_TOKEN"), "Telegram bot token")
	allowedUserID := fs.Int64("allowed-user", envInt64FirstOrDefault(0, "ALLOWED_TELEGRAM_USER_ID", "VCLAW_TELEGRAM_ALLOWED_USER_IDS"), "allowed Telegram user id")
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
		return fmt.Errorf("TELEGRAM_BOT_TOKEN or VCLAW_TELEGRAM_BOT_TOKEN is required")
	}
	if *allowedUserID == 0 {
		return fmt.Errorf("ALLOWED_TELEGRAM_USER_ID or VCLAW_TELEGRAM_ALLOWED_USER_IDS is required")
	}

	// Prevent duplicate instances: bind a local TCP port derived from the bot
	// token. The OS releases the port automatically when the process dies, so
	// zombie processes from a previous go run are detected immediately.
	lock, err := acquireTelegramLock(*botToken)
	if err != nil {
		return err
	}
	defer lock.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	metrics := monitoring.NewMetrics(time.Now())
	bundle, err := app.BuildRuntime(ctx, app.AgentRuntimeConfig{
		DataDir:                    *dataDir,
		OpenAIAPIKey:               envFirst("OPENAI_API_KEY", "LLM_API_KEY"),
		OpenAIModel:                envFirst("OPENAI_MODEL", "LLM_MODEL"),
		OpenAIBaseURL:              envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"),
		CompactorModel:             envFirst("VCLAW_COMPACTOR_MODEL"),
		Timezone:                   envOrDefault("VCLAW_TIMEZONE", "Asia/Ho_Chi_Minh"),
		DatabaseURL:                envFirst("DATABASE_URL"),
		MaxIterations:              *maxIterations,
		GoogleToolsMode:            *googleToolsMode,
		WebToolsMode:               *webToolsMode,
		GoogleCredentialsPath:      *credentialsPath,
		GoogleTokenPath:            *googleTokenPath,
		TavilyAPIKey:               envFirst("TAVILY_API_KEY", "TALIVY_API_KEY"),
		TavilyBaseURL:              envFirst("TAVILY_BASE_URL"),
		EnableSandboxTools:         true,
		SandboxWorkspaceDir:        envOrDefault("VCLAW_SANDBOX_WORKSPACE_DIR", ".sandbox-workspace"),
		SandboxImage:               envFirst("VCLAW_SANDBOX_IMAGE"),
		LangfusePublicKey:          envFirst("LANGFUSE_PUBLIC_KEY"),
		LangfuseSecretKey:          envFirst("LANGFUSE_SECRET_KEY"),
		LangfuseHost:               envFirst("LANGFUSE_HOST"),
		LangfuseProjectID:          envFirst("LANGFUSE_PROJECT_ID"),
		Logger:                     logger,
		Observer:                   metrics,
		ParallelExecutionEnabled:   os.Getenv("VCLAW_PARALLEL_ENABLED") == "true",
		ParallelMaxWorkers:         envIntOrDefault("VCLAW_PARALLEL_MAX_WORKERS", 4),
		ParallelToolTimeoutDefault: envDurationOrDefault("VCLAW_PARALLEL_TOOL_TIMEOUT", 30*time.Second),
	})
	if err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := startMetricsServer(runCtx, logger, bundle, metrics, "telegram"); err != nil {
		return err
	}
	stopPolicyReload := startPolicyReloadWatcher(runCtx, logger, bundle.PolicyStore)
	defer stopPolicyReload()

	logger.Info("starting vclaw telegram runtime", "model", bundle.Model, "google_tools", *googleToolsMode, "web_tools", *webToolsMode)
	bot := telegram.New(*botToken, *allowedUserID, *dataDir, bundle.PolicyStore, agent.NewRuntimeMessenger(bundle.Runtime), logger)
	if err := bot.Run(runCtx); err != nil && err != context.Canceled {
		return fmt.Errorf("telegram bot stopped: %w", err)
	}
	return nil
}

func envInt64OrDefault(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64FirstOrDefault(fallback int64, names ...string) int64 {
	value := firstCSV(envFirst(names...))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFirst(names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			return value
		}
	}
	return ""
}

func firstCSV(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

// acquireTelegramLock binds a local TCP port derived from the bot token.
// If the port is already in use, another instance is running. The OS releases
// the port automatically when the process exits, even if killed ungracefully.
func acquireTelegramLock(token string) (net.Listener, error) {
	port := telegramLockPort(token)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf(
			"another vclaw telegram instance is already running for this bot token (port %d in use).\n"+
				"Find and stop it with: Get-Process | Where-Object {$_.Name -like '*vclaw*'} | Stop-Process",
			port,
		)
	}
	return ln, nil
}

// telegramLockPort derives a stable port in the ephemeral range 49152–65534
// from the bot token so that different tokens do not conflict.
func telegramLockPort(token string) int {
	h := fnv.New32a()
	h.Write([]byte(token))
	return 49152 + int(h.Sum32()%16382)
}

func printTelegramUsage() {
	fmt.Println(`Usage:
  vclaw telegram run [--google-tools auto|required|off] [--web-tools auto|required|off]

Environment:
  OPENAI_API_KEY               Required for the real AI provider.
  OPENAI_MODEL                 Optional model override.
  TELEGRAM_BOT_TOKEN           Required unless --token is passed. VCLAW_TELEGRAM_BOT_TOKEN is also accepted.
  ALLOWED_TELEGRAM_USER_ID     Required unless --allowed-user is passed. VCLAW_TELEGRAM_ALLOWED_USER_IDS is also accepted.`)
}
