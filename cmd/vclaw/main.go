package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vclaw/internal/connectors/google"
	"vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/chat"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
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
	case "telegram":
		return runTelegram(ctx, args[1:])
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
		calClient, err := calendar.NewClient(ctx, httpClient)
		if err != nil {
			return fmt.Errorf("failed to create calendar client: %w", err)
		}

		timeMin := time.Now()
		timeMax := timeMin.AddDate(0, 1, 0) // Next 1 month

		events, err := calClient.ListEvents(ctx, timeMin, timeMax, "")
		if err != nil {
			return fmt.Errorf("calendar smoke test failed: %w", err)
		}
		if len(events) == 0 {
			fmt.Println("- no upcoming events found")
		}
		for i, event := range events {
			if i >= 10 {
				break
			}
			fmt.Printf("- %s | %s\n", event.StartTime.Format(time.RFC3339), event.Title)
		}

		fmt.Println()
		fmt.Println("Google Chat spaces:")
		spacesOutput, err := chat.ListSpaces(ctx, httpClient, 10, "")
		if err != nil {
			return fmt.Errorf("chat smoke test failed: %w", err)
		}
		spaces := spacesOutput.Spaces
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
	case "people":
		return runGooglePeople(ctx, args[1:])

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

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
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
  vclaw agent chat
  vclaw telegram run
  vclaw google auth
  vclaw google smoke [-chat-space spaces/AAAA...]
  vclaw google gmail <list|get|list-threads|get-thread|create-draft|update-draft|send-draft|reply-draft|forward-draft|download-attachments|modify-message>
  vclaw google people <search-directory>
  vclaw google chat <list-spaces|list-members|find-spaces-by-members|list-messages|send|update-message|delete-message|create-space|add-member|remove-member>`)
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
	printGooglePeopleUsage()
	fmt.Println()
	printGoogleChatUsage()
}

func printGoogleGmailUsage() {
	fmt.Println(`Google Gmail commands:
  vclaw google gmail list [-query "from:abc@example.com"] [-from abc@example.com] [-subject "weekly report"] [-after 2026-06-01] [-before 2026-06-30] [-labels INBOX,UNREAD] [-max-results 10] [-page-token token]
      List emails with optional Gmail search and filters.

  vclaw google gmail get -id <message-id> [-render text|raw-html] [-full]
      Read one email in detail with safe rendered text (default) or raw HTML output.

  vclaw google gmail list-threads [-query "from:abc@example.com"] [-labels INBOX] [-max-results 10]
      List Gmail threads.

  vclaw google gmail get-thread -id <thread-id> [-render text|raw-html] [-full]
      Read one Gmail thread.

  vclaw google gmail create-draft -to a@example.com [-cc b@example.com] -subject "Hello" -text "Body" [-html "<p>Body</p>"]
      Create a Gmail draft.

  vclaw google gmail update-draft -id <draft-id> -to a@example.com -subject "Hello" -text "Body"
      Update a Gmail draft.

  vclaw google gmail send-draft -id <draft-id>
      Send an existing Gmail draft.

  vclaw google gmail reply-draft -id <message-id> -to a@example.com -text "Reply body"
      Create a reply draft. Use -thread without -id to draft in a known thread.

  vclaw google gmail forward-draft -id <message-id> -to a@example.com -text "Forward note"
      Create a forward draft without forwarding original attachments.

  vclaw google gmail download-attachments -id <message-id> -output-dir <dir> [-attachment-ids att1,att2]
      Download Gmail attachments to a local directory.

  vclaw google gmail modify-message -id <message-id> -action markRead|markUnread|star|unstar|archive|moveToInbox|addLabels|removeLabels [-labels LABEL1,LABEL2]
      Modify Gmail message labels.`)
}
