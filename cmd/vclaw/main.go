package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"vclaw/internal/connectors/google"
	"vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/chat"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gmailtool "vclaw/internal/tools/office/gmail"
)

const (
	defaultCredentialsPath = "configs/google/credentials.json"
	defaultTokenPath       = "configs/google/token.json"
	defaultEnvPath         = ".env"
)

func main() {
	if err := loadDotEnv(defaultEnvPath); err != nil {
		fmt.Fprintf(os.Stderr, "vclaw: load .env: %v\n", err)
		os.Exit(1)
	}
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
	case "agent":
		return runAgent(ctx, args[1:])
	case "google":
		return runGoogle(ctx, args[1:])
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
		labels, err := gmailconnector.ListLabels(ctx, httpClient, "me")
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
			message, err := chat.CreateTextMessage(ctx, httpClient, *chatSpace, *chatText, chat.MessageCreateOptions{})
			if err != nil {
				return fmt.Errorf("chat send smoke test failed: %w", err)
			}
			fmt.Println()
			fmt.Printf("Sent Google Chat message: %s\n", message.Name)
		}

		return nil
	case "gmail":
		return runGoogleGmail(ctx, args[1:])
	case "chat":
		return runGoogleChat(ctx, args[1:])

	case "help", "-h", "--help":
		printGoogleUsage()
		return nil
	default:
		return fmt.Errorf("unknown google command %q", args[0])
	}
}

func runGoogleGmail(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleGmailUsage()
		return nil
	}

	switch args[0] {
	case "list":
		fs := newGoogleFlagSet("gmail list")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		query := fs.String("query", "", "raw Gmail query")
		from := fs.String("from", "", "filter by sender address")
		subject := fs.String("subject", "", "filter by subject")
		after := fs.String("after", "", "filter emails after date (YYYY-MM-DD)")
		before := fs.String("before", "", "filter emails before date (YYYY-MM-DD)")
		labels := fs.String("labels", "", "comma separated label IDs")
		maxResults := fs.Int64("max-results", 10, "number of emails to return (1-50)")
		pageToken := fs.String("page-token", "", "optional Gmail page token")
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

		service := gmailtool.NewService(gmailconnector.NewClient(httpClient))
		output, toolErr := service.ListEmails(ctx, gmailtool.ListEmailsInput{
			UserID:     *userID,
			Query:      *query,
			From:       *from,
			Subject:    *subject,
			After:      *after,
			Before:     *before,
			LabelIDs:   splitCSV(*labels),
			MaxResults: *maxResults,
			PageToken:  *pageToken,
		})
		if toolErr != nil {
			return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
		}

		fmt.Printf("Resolved query: %s\n", output.Query)
		if len(output.Messages) == 0 {
			fmt.Println("No emails found.")
		}
		for _, msg := range output.Messages {
			fmt.Printf("- %s | %s | %s\n", msg.ID, msg.From, msg.Subject)
			fmt.Printf("  Date: %s\n", msg.Date)
			fmt.Printf("  Snippet: %s\n", msg.Snippet)
		}
		if strings.TrimSpace(output.NextPageToken) != "" {
			fmt.Printf("Next page token: %s\n", output.NextPageToken)
		}
		return nil

	case "get":
		fs := newGoogleFlagSet("gmail get")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		messageID := fs.String("id", "", "Gmail message ID (required)")
		renderMode := fs.String("render", "text", "body render mode: text or raw-html")
		fullBody := fs.Bool("full", false, "print full body output instead of preview")
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

		service := gmailtool.NewService(gmailconnector.NewClient(httpClient))
		output, toolErr := service.GetEmail(ctx, gmailtool.GetEmailInput{
			UserID:       *userID,
			MessageID:    *messageID,
			RenderMode:   *renderMode,
			Full:         *fullBody,
			PreviewChars: 0,
		})
		if toolErr != nil {
			return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
		}

		msg := output.Message
		fmt.Printf("ID: %s\n", msg.ID)
		fmt.Printf("Thread: %s\n", msg.ThreadID)
		fmt.Printf("From: %s\n", msg.From)
		fmt.Printf("To: %s\n", msg.To)
		fmt.Printf("Subject: %s\n", msg.Subject)
		fmt.Printf("Date: %s\n", msg.Date)
		fmt.Printf("Snippet: %s\n", msg.Snippet)
		fmt.Println()

		if output.Display.Mode == gmailtool.RenderModeText {
			fmt.Printf("Display source: %s\n", output.Display.Source)
			fmt.Println("Body (rendered text):")
		} else {
			fmt.Println("Body (raw html):")
		}
		fmt.Println(output.Display.Text)
		if output.Display.Truncated {
			fmt.Printf("Preview chars: %d\n", output.Display.PreviewChars)
		}

		if len(msg.Attachments) > 0 {
			fmt.Println()
			fmt.Println("Attachments:")
			for _, attachment := range msg.Attachments {
				fmt.Printf("- %s | %s | %d bytes | %s\n", attachment.Filename, attachment.MimeType, attachment.Size, attachment.AttachmentID)
			}
		}
		return nil

	case "help", "-h", "--help":
		printGoogleGmailUsage()
		return nil
	default:
		return fmt.Errorf("unknown google gmail command %q", args[0])
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

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=value", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d: env key is empty", path, lineNumber)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = parseDotEnvValue(strings.TrimSpace(value))
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%s:%d: set %s: %w", path, lineNumber, key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func parseDotEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func splitCSV(value string) []string {
	items := strings.Split(value, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func printUsage() {
	fmt.Println(`Usage:
  vclaw agent -prompt "..."
  vclaw google auth
  vclaw google smoke [-chat-space spaces/AAAA...]
  vclaw google gmail <list|get>
  vclaw google chat <list-spaces|list-messages|send|update-message|delete-message|create-space|add-member|remove-member>`)
}

func printGoogleUsage() {
	fmt.Println(`Usage:
  vclaw google auth
      Run the OAuth user flow and save a local refresh token.

  vclaw google smoke
      List Gmail labels, upcoming Calendar events, and Google Chat spaces.

  vclaw google smoke -chat-space spaces/AAAA...
      Also send a text-only smoke-test message to the given Chat space.`)

	fmt.Println()
	printGoogleGmailUsage()
	fmt.Println()
	printGoogleChatUsage()
}

func printGoogleGmailUsage() {
	fmt.Println(`Google Gmail commands:
  vclaw google gmail list [-query "from:abc@example.com"] [-from abc@example.com] [-subject "weekly report"] [-after 2026-06-01] [-before 2026-06-30] [-labels INBOX,UNREAD] [-max-results 10] [-page-token token]
      List emails with optional Gmail search and filters.

  vclaw google gmail get -id <message-id> [-render text|raw-html] [-full]
      Read one email in detail with safe rendered text (default) or raw HTML output.`)
}
