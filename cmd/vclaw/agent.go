package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
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
	fmt.Fprintln(os.Stderr, "Type /exit to quit, /new to start a new session, /stop to cancel the current run.")
	return runAgentChatLoop(ctx, os.Stdin, os.Stderr, agent.NewRuntimeMessenger(bundle.Runtime), sessionID, *channel, *jsonOutput, *trace, time.Now)
}

type agentChatMessenger interface {
	HandleMessage(context.Context, contracts.UserMessage) (contracts.AgentResponse, error)
	CancelSession(string) bool
}

type chatRunResult struct {
	response contracts.AgentResponse
	err      error
}

type chatLineResult struct {
	text string
	err  error
}

func runAgentChatLoop(ctx context.Context, input io.Reader, prompt io.Writer, messenger agentChatMessenger, sessionID *string, channel string, jsonOutput bool, trace bool, now func() time.Time) error {
	scanner := bufio.NewScanner(input)
	lines := make(chan chatLineResult)
	results := make(chan chatRunResult, 1)
	var activeCancel context.CancelFunc
	var activeMu sync.Mutex

	go func() {
		defer close(lines)
		for scanner.Scan() {
			lines <- chatLineResult{text: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			lines <- chatLineResult{err: err}
		}
	}()

	cancelActive := func() bool {
		activeMu.Lock()
		cancel := activeCancel
		activeMu.Unlock()
		if cancel == nil {
			return false
		}
		cancel()
		messenger.CancelSession(*sessionID)
		return true
	}

	setActiveCancel := func(cancel context.CancelFunc) {
		activeMu.Lock()
		activeCancel = cancel
		activeMu.Unlock()
	}

	clearActiveCancel := func() {
		activeMu.Lock()
		activeCancel = nil
		activeMu.Unlock()
	}

	fmt.Fprint(prompt, "\nYou> ")
	for {
		select {
		case result := <-results:
			if result.err != nil {
				return result.err
			}
			fmt.Println()
			printAgentResponse(result.response, jsonOutput, trace)
			fmt.Fprint(prompt, "\nYou> ")
			continue
		case line, ok := <-lines:
			if !ok {
				cancelActive()
				return nil
			}
			if line.err != nil {
				cancelActive()
				return line.err
			}
			text := strings.TrimSpace(line.text)
			if err := handleAgentChatLine(ctx, text, prompt, messenger, sessionID, channel, jsonOutput, trace, now, results, cancelActive, setActiveCancel, func() bool {
				activeMu.Lock()
				busy := activeCancel != nil
				activeMu.Unlock()
				return busy
			}, clearActiveCancel); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
}

func handleAgentChatLine(ctx context.Context, text string, prompt io.Writer, messenger agentChatMessenger, sessionID *string, channel string, jsonOutput bool, trace bool, now func() time.Time, results chan<- chatRunResult, cancelActive func() bool, setActiveCancel func(context.CancelFunc), isBusy func() bool, clearActiveCancel func()) error {
	if text == "" {
		fmt.Fprint(prompt, "\nYou> ")
		return nil
	}
	switch text {
	case "/exit", "/quit":
		cancelActive()
		return io.EOF
	case "/new":
		if cancelActive() {
			fmt.Fprintln(prompt, "Đã hủy lệnh đang chạy.")
		}
		*sessionID = "dev_" + now().UTC().Format("20060102T150405")
		fmt.Fprintf(prompt, "Session: %s\n", *sessionID)
		fmt.Fprint(prompt, "\nYou> ")
		return nil
	case "/stop", "/cancel":
		if cancelActive() {
			fmt.Fprintln(prompt, "Đã hủy lệnh đang chạy.")
		} else {
			fmt.Fprintln(prompt, "Không có lệnh nào đang chạy.")
		}
		fmt.Fprint(prompt, "\nYou> ")
		return nil
	}

	if isBusy() {
		fmt.Fprintln(prompt, "Đang có lệnh chạy. Dùng /stop để hủy trước khi gửi lệnh mới.")
		fmt.Fprint(prompt, "\nYou> ")
		return nil
	}

	runCtx, runCancel := context.WithCancel(ctx)
	setActiveCancel(runCancel)
	message := contracts.UserMessage{
		RequestID: "req_" + now().UTC().Format("20060102T150405.000000000"),
		SessionID: *sessionID,
		Channel:   channel,
		Text:      text,
		Timestamp: now(),
		Metadata:  map[string]any{"source": "vclaw agent chat"},
	}
	go func() {
		response, err := messenger.HandleMessage(runCtx, message)
		clearActiveCancel()
		results <- chatRunResult{response: response, err: err}
	}()
	return nil
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

	if reason := agent.ExitReason(response); reason != "" {
		fmt.Fprintf(os.Stderr, "\n[exit] %s\n", reason)
	}

	if response.Plan != nil && len(response.Plan.Steps) > 0 {
		fmt.Fprintln(os.Stderr, "\n[plan]")
		for _, step := range response.Plan.Steps {
			marker := "  "
			switch step.Status {
			case "completed":
				marker = "✓ "
			case "in_progress":
				marker = "▶ "
			case "pending":
				marker = "· "
			}
			fmt.Fprintf(os.Stderr, "  %s%s\n", marker, step.Description)
		}
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
