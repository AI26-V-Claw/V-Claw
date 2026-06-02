package main

import (
	"flag"
	"testing"
)

func TestGmailDraftFlagsParseRecipients(t *testing.T) {
	fs := flag.NewFlagSet("gmail create-draft", flag.ContinueOnError)
	flags := addGmailDraftFlags(fs)

	err := fs.Parse([]string{
		"-user", "me",
		"-to", "alice@example.com,bob@example.com",
		"-cc", "carol@example.com",
		"-subject", "Hello",
		"-text", "Draft body",
	})
	if err != nil {
		t.Fatalf("parse draft flags: %v", err)
	}

	input := gmailDraftInputFromFlags(flags)
	if len(input.To) != 2 || input.To[0] != "alice@example.com" || input.To[1] != "bob@example.com" {
		t.Fatalf("unexpected To recipients: %#v", input.To)
	}
	if len(input.Cc) != 1 || input.Cc[0] != "carol@example.com" {
		t.Fatalf("unexpected Cc recipients: %#v", input.Cc)
	}
	if input.Subject != "Hello" || input.TextBody != "Draft body" {
		t.Fatalf("unexpected draft input: %#v", input)
	}
}
