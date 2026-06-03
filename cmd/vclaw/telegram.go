package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"vclaw/internal/agent"
	"vclaw/internal/channels/telegram"
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
	googleToolsMode := fs.String("google-tools", envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", googleToolsAuto), "Google Workspace tool mode: auto, required, or off")
	credentialsPath := fs.String("credentials", defaultCredentialsPath, "Google OAuth desktop client credentials JSON")
	googleTokenPath := fs.String("google-token", defaultTokenPath, "Google OAuth token cache path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*botToken) == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN or VCLAW_TELEGRAM_BOT_TOKEN is required")
	}
	if *allowedUserID == 0 {
		return fmt.Errorf("ALLOWED_TELEGRAM_USER_ID or VCLAW_TELEGRAM_ALLOWED_USER_IDS is required")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	bundle, err := newAgentRuntime(ctx, agentRuntimeOptions{
		MaxIterations:   *maxIterations,
		GoogleToolsMode: *googleToolsMode,
		CredentialsPath: *credentialsPath,
		GoogleTokenPath: *googleTokenPath,
		Logger:          logger,
	})
	if err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting vclaw telegram runtime", "model", bundle.Model, "google_tools", *googleToolsMode)
	bot := telegram.New(*botToken, *allowedUserID, *dataDir, agent.NewRuntimeMessenger(bundle.Runtime), logger)
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

func printTelegramUsage() {
	fmt.Println(`Usage:
  vclaw telegram run [--google-tools auto|required|off]

Environment:
  OPENAI_API_KEY               Required for the real AI provider.
  OPENAI_MODEL                 Optional model override.
  TELEGRAM_BOT_TOKEN           Required unless --token is passed. VCLAW_TELEGRAM_BOT_TOKEN is also accepted.
  ALLOWED_TELEGRAM_USER_ID     Required unless --allowed-user is passed. VCLAW_TELEGRAM_ALLOWED_USER_IDS is also accepted.`)
}
