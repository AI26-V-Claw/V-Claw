package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"vclaw/internal/monitoring"
)

func runStatus(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("status does not accept arguments")
	}

	health, err := loadStatusHealth(ctx)
	if err != nil {
		return err
	}
	printStatusHealth(health)
	if err := printLatestRunTrace(ctx); err != nil {
		return err
	}
	return nil
}

func printLatestRunTrace(ctx context.Context) error {
	run, err := monitoring.QueryLatestRun(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")))
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.TraceURL) == "" {
		return nil
	}
	fmt.Println()
	fmt.Printf("Latest run trace: %s\n", run.TraceURL)
	return nil
}

func loadStatusHealth(ctx context.Context) (monitoring.HealthResponse, error) {
	port := strings.TrimSpace(os.Getenv("METRICS_PORT"))
	if port == "" {
		port = "8080"
	}
	health, err := monitoring.FetchHealth(ctx, "http://127.0.0.1:"+port)
	if err != nil {
		return monitoring.HealthResponse{}, fmt.Errorf("Không kết nối được tới monitoring server. Hãy chạy vclaw run trước.")
	}
	return health, nil
}

func printStatusHealth(health monitoring.HealthResponse) {
	fmt.Printf("Status:    %s\n", health.Status)
	fmt.Printf("Uptime:    %s\n", health.Uptime)
	fmt.Printf("Checked:   %s\n", health.CheckedAt)
	fmt.Println()

	for _, key := range []string{"postgres", "llm_provider", "google_oauth", "tavily", "channel", "tool_registry"} {
		component, ok := health.Components[key]
		if !ok {
			continue
		}
		fmt.Printf("%-14s %-8s", key, component.Status)
		switch {
		case component.LatencyMS > 0:
			fmt.Printf(" %dms", component.LatencyMS)
		case component.ToolCount > 0:
			fmt.Printf(" %d tools", component.ToolCount)
		}
		fmt.Println()
	}
}
