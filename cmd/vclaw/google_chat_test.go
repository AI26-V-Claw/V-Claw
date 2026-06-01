package main

import (
	"context"
	"strings"
	"testing"
)

func TestValidateWorkspaceMemberEmailsAllowsConfiguredDomain(t *testing.T) {
	err := validateWorkspaceMemberEmails(
		[]string{"alice@example.com", "users/bob@example.com"},
		[]string{"example.com"},
	)
	if err != nil {
		t.Fatalf("expected valid Workspace members, got %v", err)
	}
}

func TestValidateWorkspaceMemberEmailsRejectsExternalDomain(t *testing.T) {
	err := validateWorkspaceMemberEmails(
		[]string{"alice@example.com", "external@gmail.com"},
		[]string{"example.com"},
	)
	if err == nil {
		t.Fatal("expected external domain rejection")
	}
}

func TestValidateWorkspaceMemberEmailsRequiresDomainConfig(t *testing.T) {
	err := validateWorkspaceMemberEmails([]string{"alice@example.com"}, nil)
	if err == nil {
		t.Fatal("expected missing domain config error")
	}
}

func TestValidateWorkspaceMemberEmailsRejectsOpaqueUserID(t *testing.T) {
	err := validateWorkspaceMemberEmails([]string{"users/123456789"}, []string{"example.com"})
	if err == nil {
		t.Fatal("expected opaque user id rejection")
	}
}

func TestValidateCreateSpaceMembersRequiresTwoGroupMembers(t *testing.T) {
	err := validateCreateSpaceMembers("GROUP_CHAT", []string{"alice@example.com"})
	if err == nil {
		t.Fatal("expected group chat member count error")
	}
}

func TestValidateCreateSpaceMembersAllowsDirectMessageWithOneMember(t *testing.T) {
	err := validateCreateSpaceMembers("DIRECT_MESSAGE", []string{"alice@example.com"})
	if err != nil {
		t.Fatalf("expected direct message with one member to pass, got %v", err)
	}
}

func TestRejectUnexpectedArgs(t *testing.T) {
	err := rejectUnexpectedArgs([]string{"bob@example.com"})
	if err == nil {
		t.Fatal("expected unexpected arg error")
	}
}

func TestSendCardFailsBeforeOAuth(t *testing.T) {
	err := runGoogleChat(context.Background(), []string{
		"send-card",
		"-space", "spaces/A",
		"-title", "Meeting Reminder",
		"-text", "Team sync starts at 10:00.",
	})
	if err == nil {
		t.Fatal("expected unsupported card error")
	}
	if !strings.Contains(err.Error(), "card messages are not supported") {
		t.Fatalf("expected unsupported card error, got %v", err)
	}
}
