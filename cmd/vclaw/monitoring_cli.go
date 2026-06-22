package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"vclaw/internal/app"
	"vclaw/internal/monitoring"
)

const sinceHelpText = "Lọc theo thời gian. Hỗ trợ thời lượng (1h, 24h, 7d) hoặc ngày cụ thể (2026-06-01)."

func monitoringServerConfig(ctx context.Context) monitoring.ServerConfig {
	googleToolsMode := envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", app.ToolModeAuto)
	webToolsMode := envOrDefault("VCLAW_WEB_TOOLS_MODE", app.ToolModeAuto)
	config := app.AgentRuntimeConfig{
		DataDir:               envOrDefault("DATA_DIR", "./data"),
		GoogleToolsMode:       googleToolsMode,
		GoogleCredentialsPath: envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", defaultCredentialsPath),
		GoogleTokenPath:       envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", defaultTokenPath),
		WebToolsMode:          webToolsMode,
		TavilyAPIKey:          envFirst("TAVILY_API_KEY", "TALIVY_API_KEY"),
		TavilyBaseURL:         envFirst("TAVILY_BASE_URL"),
		EnableSandboxTools:    true,
		SandboxWorkspaceDir:   envOrDefault("VCLAW_SANDBOX_WORKSPACE_DIR", ".sandbox-workspace"),
		SandboxImage:          envFirst("VCLAW_SANDBOX_IMAGE"),
	}

	toolCount := 0
	if registry, err := app.NewAgentToolRegistry(ctx, config); err == nil {
		toolCount = len(registry.ListTools())
	}

	return monitoring.ServerConfig{
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ProviderName:          providerNameFromEnv(),
		GoogleOAuthConfigured: googleOAuthConfiguredForCLI(config),
		TavilyConfigured:      strings.TrimSpace(config.TavilyAPIKey) != "",
		ChannelName:           configuredChannelName(),
		ToolCount:             toolCount,
		StartedAt:             time.Now(),
	}
}

func parseSince(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}

	if duration, err := parseSinceDuration(raw); err == nil {
		return now.Add(-duration), nil
	}

	if parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02T15:04:05", raw, time.Local); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("Giá trị --since không hợp lệ. %s", sinceHelpText)
}

func providerNameFromEnv() string {
	if strings.TrimSpace(envFirst("OPENAI_API_KEY", "LLM_API_KEY")) == "" {
		return ""
	}
	baseURL := strings.TrimSpace(envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"))
	if baseURL != "" && !strings.Contains(baseURL, "openai.com") {
		return "openai_compatible"
	}
	return "openai"
}

func configuredChannelName() string {
	if envBool("VCLAW_TELEGRAM_ENABLED", false) || strings.TrimSpace(envFirst("TELEGRAM_BOT_TOKEN", "VCLAW_TELEGRAM_BOT_TOKEN")) != "" {
		return "telegram"
	}
	return "cli"
}

func googleOAuthConfiguredForCLI(config app.AgentRuntimeConfig) bool {
	mode := strings.TrimSpace(config.GoogleToolsMode)
	if mode == app.ToolModeOff {
		return false
	}
	if mode == app.ToolModeRequired {
		return true
	}
	return fileExists(config.GoogleCredentialsPath) && fileExists(config.GoogleTokenPath)
}

func fileExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func parseSinceDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty since")
	}
	if strings.HasSuffix(raw, "d") {
		days, err := time.ParseDuration(strings.TrimSuffix(raw, "d") + "h")
		if err == nil {
			return days * 24, nil
		}
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", raw, err)
	}
	return duration, nil
}
