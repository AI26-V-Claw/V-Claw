package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"vclaw/internal/app"
	"vclaw/internal/monitoring"
)

func startMetricsServer(ctx context.Context, logger *slog.Logger, bundle app.RuntimeBundle, metrics *monitoring.Metrics, channelName string) error {
	return monitoring.StartServer(ctx, monitoring.ServerConfig{
		Logger:                logger,
		Metrics:               metrics,
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ProviderName:          providerName(bundle),
		GoogleOAuthConfigured: bundle.GoogleOAuthConfigured,
		TavilyConfigured:      bundle.TavilyConfigured,
		ChannelName:           channelName,
		ToolCount:             len(bundle.Registry.ListTools()),
		StartedAt:             metrics.StartedAt(),
	})
}

func providerName(bundle app.RuntimeBundle) string {
	if bundle.Provider == nil {
		return ""
	}
	return bundle.Provider.Name()
}
