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
	"vclaw/internal/app"
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
	dataDir := fs.String("data-dir", envOrDefault("DATA_DIR", "./data"), "runtime data directory")
	iterationBudget := fs.Int("iteration-budget", agent.DefaultIterationBudget, "maximum agent iteration budget")
	googleToolsMode := fs.String("google-tools", envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", app.ToolModeAuto), "Google Workspace tool mode: auto, required, or off")
	webToolsMode := fs.String("web-tools", envOrDefault("VCLAW_WEB_TOOLS_MODE", app.ToolModeAuto), "Web search/fetch tool mode: auto, required, or off")
	credentialsPath := fs.String("credentials", envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", defaultCredentialsPath), "Google OAuth desktop client credentials JSON")
	googleTokenPath := fs.String("google-token", envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", defaultTokenPath), "Google OAuth token cache path")
	jsonOutput := fs.Bool("json", false, "print the full AgentResponse JSON")
	trace := fs.Bool("trace", false, "print model and exposed tool trace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("agent prompt is required")
	}

	bundle, err := app.BuildRuntime(ctx, app.AgentRuntimeConfig{
		DataDir:                    *dataDir,
		OpenAIAPIKey:               envFirst("OPENAI_API_KEY", "LLM_API_KEY"),
		OpenAIModel:                envFirst("OPENAI_MODEL", "LLM_MODEL"),
		OpenAIBaseURL:              envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"),
		CompactorModel:             envFirst("VCLAW_COMPACTOR_MODEL"),
		DatabaseURL:                envFirst("DATABASE_URL"),
		IterationBudget:            *iterationBudget,
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
		ParallelExecutionEnabled:   os.Getenv("VCLAW_PARALLEL_ENABLED") == "true",
		ParallelMaxWorkers:         envIntOrDefault("VCLAW_PARALLEL_MAX_WORKERS", 4),
		ParallelToolTimeoutDefault: envDurationOrDefault("VCLAW_PARALLEL_TOOL_TIMEOUT", 30*time.Second),
	})
	if err != nil {
		return err
	}

	response, err := agent.NewRuntimeMessenger(bundle.Runtime).HandleMessage(ctx, contracts.UserMessage{
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
	dataDir := fs.String("data-dir", envOrDefault("DATA_DIR", "./data"), "runtime data directory")
	iterationBudget := fs.Int("iteration-budget", agent.DefaultIterationBudget, "maximum agent iteration budget")
	googleToolsMode := fs.String("google-tools", envOrDefault("VCLAW_GOOGLE_TOOLS_MODE", app.ToolModeAuto), "Google Workspace tool mode: auto, required, or off")
	webToolsMode := fs.String("web-tools", envOrDefault("VCLAW_WEB_TOOLS_MODE", app.ToolModeAuto), "Web search/fetch tool mode: auto, required, or off")
	credentialsPath := fs.String("credentials", envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", defaultCredentialsPath), "Google OAuth desktop client credentials JSON")
	googleTokenPath := fs.String("google-token", envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", defaultTokenPath), "Google OAuth token cache path")
	jsonOutput := fs.Bool("json", false, "print each full AgentResponse JSON")
	trace := fs.Bool("trace", false, "print model and exposed tool trace")
	if err := fs.Parse(args); err != nil {
		return err
	}

	bundle, err := app.BuildRuntime(ctx, app.AgentRuntimeConfig{
		DataDir:                    *dataDir,
		OpenAIAPIKey:               envFirst("OPENAI_API_KEY", "LLM_API_KEY"),
		OpenAIModel:                envFirst("OPENAI_MODEL", "LLM_MODEL"),
		OpenAIBaseURL:              envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"),
		CompactorModel:             envFirst("VCLAW_COMPACTOR_MODEL"),
		DatabaseURL:                envFirst("DATABASE_URL"),
		IterationBudget:            *iterationBudget,
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
		ParallelExecutionEnabled:   os.Getenv("VCLAW_PARALLEL_ENABLED") == "true",
		ParallelMaxWorkers:         envIntOrDefault("VCLAW_PARALLEL_MAX_WORKERS", 4),
		ParallelToolTimeoutDefault: envDurationOrDefault("VCLAW_PARALLEL_TOOL_TIMEOUT", 30*time.Second),
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

	if output := response.Output; output != nil {
		printUserOutput(*output)
	} else if output := approvalOutputFromResponse(response); output != nil {
		printUserOutput(*output)
	} else if strings.TrimSpace(response.Message) != "" {
		fmt.Println(response.Message)
	}

	if trace {
		fmt.Fprintf(os.Stdout, "Status: %s\n", response.Status)
		if len(response.ToolResults) > 0 {
			fmt.Println("Tool results:")
			for _, result := range response.ToolResults {
				fmt.Printf("- %s success=%t\n", result.ToolName, result.Success)
				if result.Error != nil {
					fmt.Printf("  error=%s: %s\n", result.Error.Code, result.Error.Message)
				}
			}
		}
		if len(response.Data) > 0 {
			data, err := json.MarshalIndent(response.Data, "", "  ")
			if err == nil {
				fmt.Println("Trace:")
				fmt.Println(string(data))
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

	if response.Error != nil && response.Status == contracts.AgentStatusFailed {
		fmt.Fprintln(os.Stderr, response.Error.Message)
	}
}

func approvalOutputFromResponse(response contracts.AgentResponse) *contracts.UserOutput {
	if response.ApprovalRequest == nil {
		return nil
	}
	approval := response.ApprovalRequest
	meta := map[string]any{
		"approvalId": approval.ApprovalID,
	}
	if !approval.ExpiresAt.IsZero() {
		meta["expiresAt"] = approval.ExpiresAt.Format(time.RFC3339)
	}
	if strings.TrimSpace(approval.ParentApprovalID) != "" {
		meta["parentApprovalId"] = approval.ParentApprovalID
	}
	return &contracts.UserOutput{
		Kind: contracts.UserOutputKindApproval,
		Text: renderCLIApprovalRequest(*approval),
		Meta: meta,
	}
}

func renderCLIApprovalRequest(approval contracts.ApprovalRequest) string {
	var lines []string
	lines = append(lines, "Cần xác nhận trước khi thực hiện.")
	lines = append(lines, "")
	if strings.TrimSpace(approval.Summary) != "" {
		lines = append(lines, "Tóm tắt: "+strings.TrimSpace(approval.Summary))
	}
	if strings.TrimSpace(approval.Details) != "" {
		lines = append(lines, "Chi tiết: "+strings.TrimSpace(approval.Details))
	}
	if strings.TrimSpace(approval.ToolCall.ToolName) != "" {
		lines = append(lines, "Tool: "+strings.TrimSpace(approval.ToolCall.ToolName))
	}
	if approval.RiskLevel != "" {
		lines = append(lines, "Risk: "+string(approval.RiskLevel))
	}
	if len(approval.ToolCall.Input) > 0 {
		data, err := json.MarshalIndent(approval.ToolCall.Input, "", "  ")
		if err == nil {
			lines = append(lines, "Input:")
			lines = append(lines, string(data))
		}
	}
	return strings.Join(lines, "\n")
}

func printUserOutput(output contracts.UserOutput) {
	if output.Kind == contracts.UserOutputKindError {
		if text := strings.TrimSpace(output.Text); text != "" {
			fmt.Fprintln(os.Stderr, text)
		}
		return
	}

	text := strings.TrimSpace(output.Text)
	if text != "" {
		fmt.Println(text)
	}

	if output.ArtifactRef != nil {
		ref := output.ArtifactRef
		label := strings.TrimSpace(ref.Label)
		uri := strings.TrimSpace(ref.URI)
		switch {
		case label != "" && uri != "":
			fmt.Printf("%s: %s\n", label, uri)
		case label != "":
			fmt.Println(label)
		case uri != "":
			fmt.Println(uri)
		case strings.TrimSpace(ref.ID) != "":
			fmt.Println(ref.ID)
		}
	}

	switch output.Kind {
	case contracts.UserOutputKindApproval:
		if approvalID, ok := output.Meta["approvalId"].(string); ok && strings.TrimSpace(approvalID) != "" {
			fmt.Printf("Approval ID: %s\n", strings.TrimSpace(approvalID))
		}
		if expiresAt, ok := output.Meta["expiresAt"].(string); ok && strings.TrimSpace(expiresAt) != "" {
			fmt.Printf("Expires At: %s\n", strings.TrimSpace(expiresAt))
		}
		fmt.Println("Reply with: approve, reject, revise <comment>")
	case contracts.UserOutputKindExpired:
		// The text already explains the expiry. Nothing else to add.
	}
}
