package main

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/connectors/google"
	chatconnector "vclaw/internal/connectors/google/chat"
	googleoauth "vclaw/internal/connectors/google/oauth"
	chattool "vclaw/internal/tools/office/chat"
)

func runGoogleChat(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleChatUsage()
		return nil
	}

	switch args[0] {
	case "list-spaces":
		fs := newGoogleFlagSet("chat list-spaces")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		pageSize := fs.Int64("page-size", 10, "number of spaces to return")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		spaces, err := client.ListSpaces(ctx, *pageSize)
		if err != nil {
			return err
		}
		if len(spaces) == 0 {
			fmt.Println("No Google Chat spaces found.")
		}
		for _, space := range spaces {
			fmt.Printf("- %s | %s | %s\n", space.Name, emptyForCLI(space.DisplayName, "(no display name)"), emptyForCLI(space.SpaceType, emptyForCLI(space.Type, "(no type)")))
			if strings.TrimSpace(space.SpaceURI) != "" {
				fmt.Printf("  URI: %s\n", space.SpaceURI)
			}
		}
		return nil

	case "list-messages":
		fs := newGoogleFlagSet("chat list-messages")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		space := fs.String("space", "", "Google Chat space resource name, for example spaces/AAAA...")
		maxResults := fs.Int64("max-results", 10, "number of messages to return (1-50)")
		pageToken := fs.String("page-token", "", "optional Chat page token")
		showDeleted := fs.Bool("show-deleted", false, "include deleted messages")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}

		service := chattool.NewService(client)
		output, toolErr := service.ListMessages(ctx, chattool.ListMessagesInput{
			Space:       *space,
			MaxResults:  *maxResults,
			PageToken:   *pageToken,
			ShowDeleted: *showDeleted,
		})
		if toolErr != nil {
			return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
		}
		if len(output.Messages) == 0 {
			fmt.Println("No Chat messages found.")
		}
		for _, message := range output.Messages {
			fmt.Printf("- %s | %s | %s\n", message.Name, emptyForCLI(message.Sender, "(no sender)"), emptyForCLI(message.Text, "(no text)"))
			if strings.TrimSpace(message.ThreadName) != "" {
				fmt.Printf("  Thread: %s\n", message.ThreadName)
			}
		}
		if strings.TrimSpace(output.NextPageToken) != "" {
			fmt.Printf("Next page token: %s\n", output.NextPageToken)
		}
		return nil

	case "send":
		fs := newGoogleFlagSet("chat send")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		space := fs.String("space", "", "Google Chat space resource name, for example spaces/AAAA...")
		text := fs.String("text", "", "text message to send")
		threadName := fs.String("thread", "", "optional thread resource name")
		threadKey := fs.String("thread-key", "", "optional thread key")
		replyOption := fs.String("reply-option", "", "optional reply option, for example REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
		attachmentPath := fs.String("attachment", "", "optional local file path to upload and attach")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		options := chatconnector.MessageCreateOptions{
			ThreadName:         *threadName,
			ThreadKey:          *threadKey,
			MessageReplyOption: *replyOption,
		}
		if strings.TrimSpace(*attachmentPath) != "" {
			token, err := uploadChatAttachment(ctx, client, *space, *attachmentPath)
			if err != nil {
				return err
			}
			options.AttachmentUploadRefs = []string{token}
		}

		message, err := client.CreateTextMessage(ctx, *space, *text, options)
		if err != nil {
			return err
		}
		fmt.Printf("Sent Google Chat message: %s\n", message.Name)
		return nil

	case "send-card":
		fs := newGoogleFlagSet("chat send-card")
		addGoogleAuthFlags(fs)
		fs.String("space", "", "Google Chat space resource name, for example spaces/AAAA...")
		fs.String("title", "", "card title")
		fs.String("subtitle", "", "optional card subtitle")
		fs.String("text", "", "card body text")
		fs.String("thread", "", "optional thread resource name")
		fs.String("thread-key", "", "optional thread key")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return fmt.Errorf("Google Chat card messages are not supported by the current user OAuth flow; use `google chat send -space ... -text ...` for text messages, or add Chat app authentication before enabling cards")

	case "update-message":
		fs := newGoogleFlagSet("chat update-message")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		name := fs.String("name", "", "message resource name, for example spaces/AAAA/messages/BBBB")
		text := fs.String("text", "", "replacement text")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		message, err := client.UpdateTextMessage(ctx, *name, *text)
		if err != nil {
			return err
		}
		fmt.Printf("Updated Google Chat message: %s\n", message.Name)
		return nil

	case "delete-message":
		fs := newGoogleFlagSet("chat delete-message")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		name := fs.String("name", "", "message resource name, for example spaces/AAAA/messages/BBBB")
		force := fs.Bool("force", false, "also delete threaded replies when supported")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		if err := client.DeleteMessage(ctx, *name, *force); err != nil {
			return err
		}
		fmt.Printf("Deleted Google Chat message: %s\n", *name)
		return nil

	case "create-space":
		fs := newGoogleFlagSet("chat create-space")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		displayName := fs.String("name", "", "display name for SPACE")
		spaceType := fs.String("type", "SPACE", "space type: SPACE, GROUP_CHAT, or DIRECT_MESSAGE")
		members := fs.String("members", "", "comma separated user emails or users/{id} resource names")
		workspaceDomains := fs.String("workspace-domains", envOrDefault("VCLAW_GOOGLE_WORKSPACE_DOMAINS", ""), "comma separated allowed Workspace email domains")
		requestID := fs.String("request-id", "", "optional idempotency request ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := rejectUnexpectedArgs(fs.Args()); err != nil {
			return err
		}

		memberUsers := splitCSV(*members)
		if err := validateCreateSpaceMembers(*spaceType, memberUsers); err != nil {
			return err
		}
		if err := validateWorkspaceMemberEmails(memberUsers, splitCSV(*workspaceDomains)); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		space, err := client.CreateSpace(ctx, chatconnector.CreateSpaceInput{
			DisplayName: *displayName,
			SpaceType:   *spaceType,
			MemberUsers: memberUsers,
			RequestID:   *requestID,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Created Google Chat space: %s | %s | %s\n", space.Name, space.DisplayName, space.SpaceType)
		return nil

	case "add-member":
		fs := newGoogleFlagSet("chat add-member")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		space := fs.String("space", "", "Google Chat space resource name, for example spaces/AAAA...")
		user := fs.String("user", "", "user email or users/{id} resource name")
		workspaceDomains := fs.String("workspace-domains", envOrDefault("VCLAW_GOOGLE_WORKSPACE_DOMAINS", ""), "comma separated allowed Workspace email domains")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := rejectUnexpectedArgs(fs.Args()); err != nil {
			return err
		}

		if err := validateWorkspaceMemberEmails([]string{*user}, splitCSV(*workspaceDomains)); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		membership, err := client.AddMember(ctx, *space, *user)
		if err != nil {
			return err
		}
		fmt.Printf("Added Google Chat member: %s | %s\n", membership.Name, membership.MemberName)
		return nil

	case "remove-member":
		fs := newGoogleFlagSet("chat remove-member")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		name := fs.String("name", "", "membership resource name, for example spaces/AAAA/members/BBBB")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		client, err := googleChatClient(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		if err := client.RemoveMember(ctx, *name); err != nil {
			return err
		}
		fmt.Printf("Removed Google Chat member: %s\n", *name)
		return nil

	case "help", "-h", "--help":
		printGoogleChatUsage()
		return nil
	default:
		return fmt.Errorf("unknown google chat command %q", args[0])
	}
}

func googleChatClient(ctx context.Context, credentialsPath string, tokenPath string) (*chatconnector.Client, error) {
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return nil, err
	}
	return chatconnector.NewClient(httpClient), nil
}

func uploadChatAttachment(ctx context.Context, client *chatconnector.Client, space string, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	filename := filepath.Base(path)
	mediaType := mime.TypeByExtension(filepath.Ext(path))
	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/octet-stream"
	}
	return client.UploadAttachment(ctx, space, filename, mediaType, file)
}

func emptyForCLI(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func rejectUnexpectedArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("unexpected argument %q; if you pass comma-separated members, do not put spaces after commas, or wrap the whole value in quotes", args[0])
}

func validateCreateSpaceMembers(spaceType string, users []string) error {
	if strings.EqualFold(strings.TrimSpace(spaceType), "GROUP_CHAT") {
		uniqueMembers := map[string]struct{}{}
		for _, user := range users {
			value := strings.ToLower(strings.TrimSpace(user))
			if value == "" {
				continue
			}
			uniqueMembers[value] = struct{}{}
		}
		if len(uniqueMembers) < 2 {
			return fmt.Errorf("GROUP_CHAT requires at least 2 unique members in -members; use DIRECT_MESSAGE for one other person")
		}
	}
	return nil
}

func validateWorkspaceMemberEmails(users []string, allowedDomains []string) error {
	if len(users) == 0 {
		return nil
	}

	allowed := normalizedDomains(allowedDomains)
	if len(allowed) == 0 {
		return fmt.Errorf("workspace domain restriction requires -workspace-domains or VCLAW_GOOGLE_WORKSPACE_DOMAINS when adding Chat members")
	}

	for _, user := range users {
		email, err := memberEmailForDomainCheck(user)
		if err != nil {
			return err
		}
		domain := emailDomain(email)
		if _, ok := allowed[domain]; !ok {
			return fmt.Errorf("member %q is outside the allowed Workspace domains: %s", user, strings.Join(mapKeys(allowed), ","))
		}
	}
	return nil
}

func normalizedDomains(domains []string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, domain := range domains {
		value := strings.ToLower(strings.TrimSpace(domain))
		value = strings.TrimPrefix(value, "@")
		if value == "" {
			continue
		}
		allowed[value] = struct{}{}
	}
	return allowed
}

func memberEmailForDomainCheck(user string) (string, error) {
	value := strings.TrimSpace(user)
	value = strings.TrimPrefix(value, "users/")
	if value == "" {
		return "", fmt.Errorf("member email is required")
	}
	if !strings.Contains(value, "@") {
		return "", fmt.Errorf("member %q must be an email address so the Workspace domain can be verified", user)
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("member %q is not a valid email address", user)
	}
	if emailDomain(value) == "" {
		return "", fmt.Errorf("member %q is not a valid email address", user)
	}
	return strings.ToLower(value), nil
}

func emailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[1]))
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func printGoogleChatUsage() {
	fmt.Println(`Google Chat commands:
  vclaw google chat list-spaces [-page-size 10]
      List Google Chat spaces.

  vclaw google chat list-messages -space spaces/AAAA... [-max-results 10] [-page-token token] [-show-deleted]
      List messages in a Google Chat space.

  vclaw google chat send -space spaces/AAAA... -text "Hello" [-thread spaces/AAAA/threads/BBBB] [-thread-key key] [-reply-option REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD] [-attachment file]
      Send a text message, optionally in a thread and optionally with one uploaded attachment.

  vclaw google chat send-card -space spaces/AAAA... -title "Title" -text "Body" [-subtitle "Subtitle"] [-thread spaces/AAAA/threads/BBBB] [-thread-key key]
      Not supported by the current user OAuth flow. Use chat send for text messages.

  vclaw google chat update-message -name spaces/AAAA/messages/BBBB -text "Updated text"
      Update a message's text.

  vclaw google chat delete-message -name spaces/AAAA/messages/BBBB [-force]
      Delete a message.

  vclaw google chat create-space -name "Project Room" [-type SPACE|GROUP_CHAT|DIRECT_MESSAGE] [-members a@example.com,b@example.com] [-workspace-domains example.com] [-request-id id]
      Create or set up a Chat space/group/direct message. Member emails must belong to the configured Workspace domain list.

  vclaw google chat add-member -space spaces/AAAA... -user user@example.com [-workspace-domains example.com]
      Add a human member to a Chat space. The email must belong to the configured Workspace domain list.

  vclaw google chat remove-member -name spaces/AAAA/members/BBBB
      Remove a member from a Chat space.`)
}
