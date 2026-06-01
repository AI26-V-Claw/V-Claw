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

func printUsage() {
	fmt.Println(`Usage:
  vclaw google auth
  vclaw google smoke [-chat-space spaces/AAAA...]`)
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
