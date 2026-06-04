package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

func runAgent(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "chat" {
		return runAgentChat(ctx, args[1:])
	}

	fs := flag.NewFlagSet("vclaw agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prompt := fs.String("prompt", "", "user prompt to send to the agent")
	sessionID := fs.String("session", "dev", "session id")
	channel := fs.String("channel", "dev-cli", "channel name")
	maxIterations := fs.Int("max-iterations", agent.DefaultMaxIterations, "maximum agent iterations")
	googleToolsMode := fs.String("google-tools", envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", googleToolsAuto), "Google Workspace tool mode: auto, required, or off")
	webToolsMode := fs.String("web-tools", envOrDefault("VCLAW_WEB_TOOLS_MODE", webToolsAuto), "Web search/fetch tool mode: auto, required, or off")
	credentialsPath := fs.String("credentials", defaultCredentialsPath, "Google OAuth desktop client credentials JSON")
	googleTokenPath := fs.String("google-token", defaultTokenPath, "Google OAuth token cache path")
	jsonOutput := fs.Bool("json", false, "print the full AgentResponse JSON")
	trace := fs.Bool("trace", false, "print model and exposed tool trace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("agent prompt is required")
	}

	bundle, err := newAgentRuntime(ctx, agentRuntimeOptions{
		MaxIterations:   *maxIterations,
		GoogleToolsMode: *googleToolsMode,
		WebToolsMode:    *webToolsMode,
		CredentialsPath: *credentialsPath,
		GoogleTokenPath: *googleTokenPath,
	})
	if err != nil {
		return err
	}

	response, err := bundle.Runtime.Run(ctx, contracts.UserMessage{
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

	printAgentResponse(response, *jsonOutput, *trace)
	if response.Error != nil && response.Status == contracts.AgentStatusFailed {
		return fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	return nil
}

func runAgentChat(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vclaw agent chat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	sessionID := fs.String("session", "dev", "session id")
	channel := fs.String("channel", "dev-cli", "channel name")
	maxIterations := fs.Int("max-iterations", agent.DefaultMaxIterations, "maximum agent iterations")
	googleToolsMode := fs.String("google-tools", envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", googleToolsAuto), "Google Workspace tool mode: auto, required, or off")
	webToolsMode := fs.String("web-tools", envOrDefault("VCLAW_WEB_TOOLS_MODE", webToolsAuto), "Web search/fetch tool mode: auto, required, or off")
	credentialsPath := fs.String("credentials", defaultCredentialsPath, "Google OAuth desktop client credentials JSON")
	googleTokenPath := fs.String("google-token", defaultTokenPath, "Google OAuth token cache path")
	jsonOutput := fs.Bool("json", false, "print each full AgentResponse JSON")
	trace := fs.Bool("trace", false, "print model and exposed tool trace")
	if err := fs.Parse(args); err != nil {
		return err
	}

	bundle, err := newAgentRuntime(ctx, agentRuntimeOptions{
		MaxIterations:   *maxIterations,
		GoogleToolsMode: *googleToolsMode,
		WebToolsMode:    *webToolsMode,
		CredentialsPath: *credentialsPath,
		GoogleTokenPath: *googleTokenPath,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "V-Claw interactive chat (model: %s, session: %s)\n", bundle.Model, *sessionID)
	fmt.Fprintln(os.Stderr, "Type /exit to quit, /new to start a new session.")
	messenger := agent.NewRuntimeMessenger(bundle.Runtime)
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "\nYou> ")
		if !scanner.Scan() {
			break
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		switch text {
		case "/exit", "/quit":
			return nil
		case "/new":
			*sessionID = "dev_" + time.Now().UTC().Format("20060102T150405")
			fmt.Fprintf(os.Stderr, "Session: %s\n", *sessionID)
			continue
		}

		response, err := messenger.HandleMessage(ctx, contracts.UserMessage{
			RequestID: "req_" + time.Now().UTC().Format("20060102T150405.000000000"),
			SessionID: *sessionID,
			Channel:   *channel,
			Text:      text,
			Timestamp: time.Now(),
			Metadata:  map[string]any{"source": "vclaw agent chat"},
		})
		if err != nil {
			return err
		}
		fmt.Println()
		printAgentResponse(response, *jsonOutput, *trace)
	}
	return scanner.Err()
}

func printAgentResponse(response contracts.AgentResponse, jsonOutput bool, trace bool) {
	if jsonOutput {
		data, err := json.MarshalIndent(response, "", "  ")
		if err == nil {
			fmt.Println(string(data))
			return
		}
	}
	fmt.Printf("Status: %s\n", response.Status)
	if response.Message != "" {
		fmt.Printf("Message: %s\n", response.Message)
	}
	if trace && len(response.Data) > 0 {
		data, err := json.MarshalIndent(response.Data, "", "  ")
		if err == nil {
			fmt.Println("Trace:")
			fmt.Println(string(data))
		}
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
		if err == nil {
			fmt.Println("Approval request:")
			fmt.Println(string(data))
		}
	}
}
