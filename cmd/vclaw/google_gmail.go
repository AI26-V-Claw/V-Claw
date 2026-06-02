package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"vclaw/internal/connectors/google"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gmailtool "vclaw/internal/tools/office/gmail"
)

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

	case "list-threads":
		fs := newGoogleFlagSet("gmail list-threads")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		query := fs.String("query", "", "raw Gmail query")
		from := fs.String("from", "", "filter by sender address")
		subject := fs.String("subject", "", "filter by subject")
		after := fs.String("after", "", "filter threads after date (YYYY-MM-DD)")
		before := fs.String("before", "", "filter threads before date (YYYY-MM-DD)")
		labels := fs.String("labels", "", "comma separated label IDs")
		maxResults := fs.Int64("max-results", 10, "number of threads to return (1-50)")
		pageToken := fs.String("page-token", "", "optional Gmail page token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ListThreads(ctx, gmailtool.ListThreadsInput{
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
		if len(output.Threads) == 0 {
			fmt.Println("No threads found.")
		}
		for _, thread := range output.Threads {
			fmt.Printf("- %s | %s\n", thread.ID, thread.Snippet)
		}
		if strings.TrimSpace(output.NextPageToken) != "" {
			fmt.Printf("Next page token: %s\n", output.NextPageToken)
		}
		return nil

	case "get-thread":
		fs := newGoogleFlagSet("gmail get-thread")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		threadID := fs.String("id", "", "Gmail thread ID (required)")
		renderMode := fs.String("render", "text", "body render mode: text or raw-html")
		fullBody := fs.Bool("full", false, "print full body output instead of preview")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.GetThread(ctx, gmailtool.GetThreadInput{
			UserID:     *userID,
			ThreadID:   *threadID,
			RenderMode: *renderMode,
			Full:       *fullBody,
		})
		if toolErr != nil {
			return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
		}
		fmt.Printf("Thread: %s\n", output.Thread.ID)
		for _, item := range output.Messages {
			fmt.Printf("\nMessage: %s | %s | %s\n", item.Message.ID, item.Message.From, item.Message.Subject)
			fmt.Println(item.Display.Text)
		}
		return nil

	case "create-draft":
		fs := newGoogleFlagSet("gmail create-draft")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		draftFlags := addGmailDraftFlags(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.CreateDraft(ctx, gmailDraftInputFromFlags(draftFlags))
		return printGmailToolOutput(output, toolErr)

	case "update-draft":
		fs := newGoogleFlagSet("gmail update-draft")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		draftID := fs.String("id", "", "Gmail draft ID (required)")
		draftFlags := addGmailDraftFlags(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.UpdateDraft(ctx, gmailtool.UpdateDraftInput{
			DraftInput: gmailDraftInputFromFlags(draftFlags),
			DraftID:    *draftID,
		})
		return printGmailToolOutput(output, toolErr)

	case "send-draft":
		fs := newGoogleFlagSet("gmail send-draft")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		draftID := fs.String("id", "", "Gmail draft ID (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.SendDraft(ctx, gmailtool.SendDraftInput{UserID: *userID, DraftID: *draftID})
		return printGmailToolOutput(output, toolErr)

	case "reply-draft":
		fs := newGoogleFlagSet("gmail reply-draft")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		messageID := fs.String("id", "", "Gmail message ID to reply to")
		draftFlags := addGmailDraftFlags(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ReplyDraft(ctx, gmailtool.ReplyDraftInput{
			DraftInput: gmailDraftInputFromFlags(draftFlags),
			MessageID:  *messageID,
		})
		return printGmailToolOutput(output, toolErr)

	case "forward-draft":
		fs := newGoogleFlagSet("gmail forward-draft")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		messageID := fs.String("id", "", "Gmail message ID to forward (required)")
		draftFlags := addGmailDraftFlags(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ForwardDraft(ctx, gmailtool.ForwardDraftInput{
			DraftInput: gmailDraftInputFromFlags(draftFlags),
			MessageID:  *messageID,
		})
		return printGmailToolOutput(output, toolErr)

	case "download-attachments":
		fs := newGoogleFlagSet("gmail download-attachments")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		messageID := fs.String("id", "", "Gmail message ID (required)")
		attachmentIDs := fs.String("attachment-ids", "", "comma separated attachment IDs; omit to download all")
		outputDir := fs.String("output-dir", "", "local output directory (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.DownloadAttachments(ctx, gmailtool.DownloadAttachmentsInput{
			UserID:        *userID,
			MessageID:     *messageID,
			AttachmentIDs: splitCSV(*attachmentIDs),
			OutputDir:     *outputDir,
		})
		return printGmailToolOutput(output, toolErr)

	case "modify-message":
		fs := newGoogleFlagSet("gmail modify-message")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		userID := fs.String("user", "me", "Gmail user ID, use me for the authorized account")
		messageID := fs.String("id", "", "Gmail message ID (required)")
		action := fs.String("action", "", "markRead, markUnread, star, unstar, archive, moveToInbox, addLabels, removeLabels")
		labels := fs.String("labels", "", "comma separated label IDs for addLabels/removeLabels")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleGmailService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ModifyMessage(ctx, gmailtool.ModifyMessageInput{
			UserID:    *userID,
			MessageID: *messageID,
			Action:    *action,
			LabelIDs:  splitCSV(*labels),
		})
		return printGmailToolOutput(output, toolErr)

	case "help", "-h", "--help":
		printGoogleGmailUsage()
		return nil
	default:
		return fmt.Errorf("unknown google gmail command %q", args[0])
	}
}

type gmailDraftFlags struct {
	userID   *string
	to       *string
	cc       *string
	bcc      *string
	subject  *string
	textBody *string
	htmlBody *string
	threadID *string
}

func addGmailDraftFlags(fs *flag.FlagSet) gmailDraftFlags {
	return gmailDraftFlags{
		userID:   fs.String("user", "me", "Gmail user ID, use me for the authorized account"),
		to:       fs.String("to", "", "comma separated To recipients"),
		cc:       fs.String("cc", "", "comma separated Cc recipients"),
		bcc:      fs.String("bcc", "", "comma separated Bcc recipients"),
		subject:  fs.String("subject", "", "email subject"),
		textBody: fs.String("text", "", "plain text body"),
		htmlBody: fs.String("html", "", "optional HTML body"),
		threadID: fs.String("thread", "", "optional Gmail thread ID"),
	}
}

func gmailDraftInputFromFlags(flags gmailDraftFlags) gmailtool.DraftInput {
	return gmailtool.DraftInput{
		UserID:   *flags.userID,
		To:       splitCSV(*flags.to),
		Cc:       splitCSV(*flags.cc),
		Bcc:      splitCSV(*flags.bcc),
		Subject:  *flags.subject,
		TextBody: *flags.textBody,
		HTMLBody: *flags.htmlBody,
		ThreadID: *flags.threadID,
	}
}

func googleGmailService(ctx context.Context, credentialsPath string, tokenPath string) (*gmailtool.Service, error) {
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return nil, err
	}
	return gmailtool.NewService(gmailconnector.NewClient(httpClient)), nil
}

func printGmailToolOutput(output any, toolErr *gmailtool.ErrorShape) error {
	if toolErr != nil {
		return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
