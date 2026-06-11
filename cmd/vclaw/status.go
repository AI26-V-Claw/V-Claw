package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

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
	return nil
}

func loadStatusHealth(ctx context.Context) (monitoring.HealthResponse, error) {
	if port, ok := os.LookupEnv("METRICS_PORT"); ok && strings.TrimSpace(port) != "" {
		baseURL := "http://127.0.0.1:" + strings.TrimSpace(port)
		requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strings.TrimSpace(port), 500*time.Millisecond); err == nil {
			return monitoring.FetchHealth(requestCtx, baseURL)
		}
	}
	return monitoring.ProbeHealth(ctx, monitoringServerConfig(ctx))
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
