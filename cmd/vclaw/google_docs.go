package main

import (
	"context"
	"encoding/json"
	"fmt"

	"vclaw/internal/connectors/google"
	docsconnector "vclaw/internal/connectors/google/docs"
	googleoauth "vclaw/internal/connectors/google/oauth"
	docstool "vclaw/internal/tools/office/docs"
)

func runGoogleDocs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleDocsUsage()
		return nil
	}

	switch args[0] {
	case "get":
		fs := newGoogleFlagSet("docs get")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		documentID := fs.String("id", "", "Google Docs document ID")
		full := fs.Bool("full", false, "print full extracted text")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDocsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.GetDocument(ctx, docstool.GetDocumentInput{DocumentID: *documentID, Full: *full})
		return printDocsToolOutput(output, toolErr)

	case "create":
		fs := newGoogleFlagSet("docs create")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		title := fs.String("title", "", "document title")
		text := fs.String("text", "", "optional initial text")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDocsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.CreateDocument(ctx, docstool.CreateDocumentInput{Title: *title, Text: *text})
		return printDocsToolOutput(output, toolErr)

	case "append":
		fs := newGoogleFlagSet("docs append")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		documentID := fs.String("id", "", "Google Docs document ID")
		text := fs.String("text", "", "text to append")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDocsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.AppendText(ctx, docstool.AppendTextInput{DocumentID: *documentID, Text: *text})
		return printDocsToolOutput(output, toolErr)

	case "help", "-h", "--help":
		printGoogleDocsUsage()
		return nil
	default:
		return fmt.Errorf("unknown google docs command %q", args[0])
	}
}

func googleDocsService(ctx context.Context, credentialsPath string, tokenPath string) (*docstool.Service, error) {
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return nil, err
	}
	client, err := docsconnector.NewClient(ctx, httpClient)
	if err != nil {
		return nil, err
	}
	return docstool.NewService(client), nil
}

func printDocsToolOutput(output any, toolErr *docstool.ErrorShape) error {
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

func printGoogleDocsUsage() {
	fmt.Println(`Google Docs commands:
  vclaw google docs get -id DOCUMENT_ID [-full]
      Read a Google Docs document and extract plain text.

  vclaw google docs create -title "Notes" [-text "Initial text"]
      Create a Google Docs document.

  vclaw google docs append -id DOCUMENT_ID -text "More text"
      Append text to a Google Docs document.`)
}
