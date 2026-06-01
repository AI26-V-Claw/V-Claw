package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

func runAgent(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vclaw agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prompt := fs.String("prompt", "", "user prompt to send to the agent")
	sessionID := fs.String("session", "dev", "session id")
	channel := fs.String("channel", "dev-cli", "channel name")
	maxIterations := fs.Int("max-iterations", agent.DefaultMaxIterations, "maximum agent iterations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("agent prompt is required")
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}
	model := envOrDefault("OPENAI_MODEL", providers.DefaultOpenAIModel)
	openAI, err := providers.NewOpenAIClient(providers.OpenAIConfig{
		APIKey: apiKey,
		Model:  model,
	})
	if err != nil {
		return err
	}

	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		return err
	}

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider:      openAI,
		Registry:      registry,
		SessionStore:  sessions.NewInMemoryStore(),
		MaxIterations: *maxIterations,
		Model:         model,
	})
	response, err := runtime.Run(ctx, contracts.UserMessage{
		RequestID: "req_" + time.Now().UTC().Format("20060102T150405.000000000"),
		SessionID: *sessionID,
		Channel:   *channel,
		Text:      *prompt,
		Timestamp: time.Now(),
		Metadata:  map[string]any{"source": "vclaw agent"},
	})
	if err != nil {
		return err
	}

	fmt.Printf("Status: %s\n", response.Status)
	if response.Message != "" {
		fmt.Printf("Message: %s\n", response.Message)
	}
	if len(response.ToolResults) > 0 {
		fmt.Println("Tool results:")
		for _, result := range response.ToolResults {
			fmt.Printf("- %s success=%t\n", result.ToolName, result.Success)
			if result.Error != nil {
				fmt.Printf("  error=%s: %s\n", result.Error.Code, result.Error.Message)
			}
		}
	}
	if response.ApprovalRequest != nil {
		data, err := json.MarshalIndent(response.ApprovalRequest, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println("Approval request:")
		fmt.Println(string(data))
	}
	if response.Error != nil && response.Status == contracts.AgentStatusFailed {
		return fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	return nil
}
