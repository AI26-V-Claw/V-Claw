package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"vclaw/internal/connectors/google"
	"vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/chat"
	"vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
)

const (
	defaultCredentialsPath = "configs/google/credentials.json"
	defaultTokenPath       = "configs/google/token.json"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vclaw: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "google":
		return runGoogle(ctx, args[1:])
	case "sandbox":
		return runSandbox(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runGoogle(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleUsage()
		return nil
	}

	switch args[0] {
	case "auth":
		fs := newGoogleFlagSet("auth")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleoauth.Client(ctx, googleoauth.Config{
			CredentialsPath: *credentialsPath,
			TokenPath:       *tokenPath,
			Scopes:          google.G1Scopes,
		})
		if err != nil {
			return err
		}
		_ = client
		fmt.Printf("Google OAuth ready. Token saved at %s\n", *tokenPath)
		return nil

	case "smoke":
		fs := newGoogleFlagSet("smoke")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		chatSpace := fs.String("chat-space", "", "optional Google Chat space resource name, for example spaces/AAAA...")
		chatText := fs.String("chat-text", "V-Claw Google Chat smoke test", "text to send when -chat-space is provided")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
			CredentialsPath: *credentialsPath,
			TokenPath:       *tokenPath,
			Scopes:          google.G1Scopes,
		})
		if err != nil {
			return err
		}

		fmt.Println("Gmail labels:")
		labels, err := gmail.ListLabels(ctx, httpClient, "me")
		if err != nil {
			return fmt.Errorf("gmail smoke test failed: %w", err)
		}
		for _, label := range labels {
			fmt.Printf("- %s (%s)\n", label.Name, label.ID)
		}

		fmt.Println()
		fmt.Println("Upcoming calendar events:")
		events, err := calendar.ListUpcomingEvents(ctx, httpClient, "primary", 10)
		if err != nil {
			return fmt.Errorf("calendar smoke test failed: %w", err)
		}
		if len(events) == 0 {
			fmt.Println("- no upcoming events found")
		}
		for _, event := range events {
			fmt.Printf("- %s | %s\n", event.Start, event.Summary)
		}

		fmt.Println()
		fmt.Println("Google Chat spaces:")
		spaces, err := chat.ListSpaces(ctx, httpClient, 10)
		if err != nil {
			return fmt.Errorf("chat smoke test failed: %w", err)
		}
		if len(spaces) == 0 {
			fmt.Println("- no spaces found")
		}
		for _, space := range spaces {
			fmt.Printf("- %s | %s\n", space.Name, space.DisplayName)
		}

		if strings.TrimSpace(*chatSpace) != "" {
			message, err := chat.CreateTextMessage(ctx, httpClient, *chatSpace, *chatText)
			if err != nil {
				return fmt.Errorf("chat send smoke test failed: %w", err)
			}
			fmt.Println()
			fmt.Printf("Sent Google Chat message: %s\n", message.Name)
		}

		return nil

	case "help", "-h", "--help":
		printGoogleUsage()
		return nil
	default:
		return fmt.Errorf("unknown google command %q", args[0])
	}
}

func newGoogleFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("vclaw google "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func addGoogleAuthFlags(fs *flag.FlagSet) (*string, *string) {
	credentialsPath := fs.String("credentials", envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", defaultCredentialsPath), "OAuth desktop client credentials JSON")
	tokenPath := fs.String("token", envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", defaultTokenPath), "OAuth token cache path")
	return credentialsPath, tokenPath
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

// ─── sandbox subcommand ───────────────────────────────────────────────────────

// runSandbox dispatches vclaw sandbox <subcommand>.
func runSandbox(args []string) error {
	if len(args) == 0 {
		printSandboxUsage()
		return nil
	}
	switch args[0] {
	case "check":
		return runSandboxCheck(args[1:])
	case "help", "-h", "--help":
		printSandboxUsage()
		return nil
	default:
		return fmt.Errorf("unknown sandbox command %q", args[0])
	}
}

// runSandboxCheck runs a policy + safety check on a command/code string and
// prints the result in human-readable Vietnamese.
//
// Usage:
//
//	vclaw sandbox check -tool run_shell -cmd "ls /workspace"
//	vclaw sandbox check -tool run_python -code "import os; os.remove('a.txt')"
//	vclaw sandbox check -tool file_ops   -op delete -path "/workspace/old.csv"
func runSandboxCheck(args []string) error {
	fs := flag.NewFlagSet("vclaw sandbox check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	tool := fs.String("tool", "run_shell", "tool name: run_shell | run_python | file_ops")
	cmd := fs.String("cmd", "", "shell command (for run_shell)")
	code := fs.String("code", "", "python code or script path (for run_python)")
	fileOp := fs.String("op", "", "file operation: list|read|write|copy|move|delete (for file_ops)")
	filePath := fs.String("path", "", "file path (for file_ops)")
	conservative := fs.Bool("conservative", false, "require confirmation for safe_write too")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// ── Build policy request ───────────────────────────────────────────────
	req := policies.Request{
		RequestID: "cli_check",
		SessionID: "cli_session",
		UserID:    "developer",
		Tool:      policies.ToolName(*tool),
		Input: policies.RequestInput{
			Command:    *cmd,
			Code:       *code,
			FileOp:     *fileOp,
			FilePath:   *filePath,
		},
		Meta: policies.RequestMeta{
			Source: "user_direct",
		},
	}

	checker := policies.NewRuleBasedChecker(policies.RuleBasedConfig{
		SafeWriteRequiresConfirm: *conservative,
	})
	result := checker.Check(req)

	// ── Safety scan ────────────────────────────────────────────────────────
	var reports []safety.DangerReport
	scanner := safety.DefaultScanner
	switch req.Tool {
	case policies.ToolRunShell:
		reports = scanner.ScanShell(*cmd)
	case policies.ToolRunPython:
		text := *code
		if strings.TrimSpace(text) == "" {
			text = req.Input.ScriptPath
		}
		reports = scanner.ScanPython(text)
	case policies.ToolFileOps:
		reports = scanner.ScanShell(*fileOp + " " + *filePath)
	}

	// ── Print result ───────────────────────────────────────────────────────
	decisionEmoji := map[policies.Decision]string{
		policies.DecisionAllow:          "✅",
		policies.DecisionNeedsApproval:  "⚠️ ",
		policies.DecisionBlock:          "🚫",
	}
	emoji := decisionEmoji[result.Decision]
	if emoji == "" {
		emoji = "?"
	}

	fmt.Println()
	fmt.Printf("══════════════════════════════════════════════════\n")
	fmt.Printf(" %s POLICY DECISION: %-20s\n", emoji, strings.ToUpper(string(result.Decision)))
	fmt.Printf("══════════════════════════════════════════════════\n")
	fmt.Printf("  Tool      : %s\n", req.Tool)
	fmt.Printf("  Risk Level: %s\n", result.RiskLevel)
	fmt.Println()

	fmt.Println("  Lý do:")
	for _, r := range result.Reasons {
		fmt.Printf("    • %s\n", r)
	}

	if len(reports) > 0 {
		fmt.Println()
		fmt.Printf("  ⚡ Safety Threats Detected (%d):\n", len(reports))
		for _, rpt := range reports {
			fmt.Printf("    [%s/%s] %s\n", rpt.Category, rpt.Severity, rpt.ExplanationVI)
			fmt.Printf("           matched: %q\n", rpt.MatchedPattern)
		}
		fmt.Println()
		fmt.Printf("  Tóm tắt: %s\n", safety.SummariseVI(reports))
	} else {
		fmt.Println()
		fmt.Println("  ✓ Không phát hiện mối đe dọa cụ thể.")
	}
	fmt.Printf("══════════════════════════════════════════════════\n\n")

	return nil
}

func printSandboxUsage() {
	fmt.Println(`Usage:
  vclaw sandbox check [options]

Options:
  -tool run_shell|run_python|file_ops   Tool to simulate (default: run_shell)
  -cmd  "<command>"                     Shell command (run_shell)
  -code "<python code>"                 Python code (run_python)
  -op   list|read|write|copy|move|delete  File operation (file_ops)
  -path "<file path>"                   Target path (file_ops)
  -conservative                         Require approval for safe_write too

Examples:
  vclaw sandbox check -tool run_shell -cmd "ls /workspace"
  vclaw sandbox check -tool run_shell -cmd "rm -rf /workspace/temp"
  vclaw sandbox check -tool run_shell -cmd "shutdown -h now"
  vclaw sandbox check -tool run_python -code "import os; os.remove('a.txt')"
  vclaw sandbox check -tool file_ops -op delete -path "/workspace/old.csv"
  vclaw sandbox check -tool run_shell -cmd "cat .env"`)
}

func printUsage() {
	fmt.Println(`Usage:
  vclaw google auth
  vclaw google smoke [-chat-space spaces/AAAA...]
  vclaw sandbox check -tool <tool> [-cmd|-code|-op|-path] "<input>"`)
}

func printGoogleUsage() {
	fmt.Println(`Usage:
  vclaw google auth
      Run the OAuth user flow and save a local refresh token.

  vclaw google smoke
      List Gmail labels, upcoming Calendar events, and Google Chat spaces.

  vclaw google smoke -chat-space spaces/AAAA...
      Also send a text-only smoke-test message to the given Chat space.`)
}
