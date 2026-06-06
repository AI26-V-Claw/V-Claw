package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
)

func TestPrintAgentResponseUsesUserOutputByDefault(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Output: &contracts.UserOutput{
				Kind: contracts.UserOutputKindMessage,
				Text: "Chao ban! Toi co the giup gi cho ban hom nay?",
			},
		}, false, false)
	})

	if !strings.Contains(stdout, "Chao ban! Toi co the giup gi cho ban hom nay?") {
		t.Fatalf("expected user output on stdout, got %q", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestPrintAgentResponsePrintsArtifactRef(t *testing.T) {
	stdout, _ := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Output: &contracts.UserOutput{
				Kind: contracts.UserOutputKindSuccess,
				Text: "Da gui tin nhan thanh cong.",
				ArtifactRef: &contracts.ArtifactRef{
					Label: "Google Chat message",
					URI:   "https://example.com/message/123",
				},
			},
		}, false, false)
	})

	if !strings.Contains(stdout, "Da gui tin nhan thanh cong.") {
		t.Fatalf("expected success message, got %q", stdout)
	}
	if !strings.Contains(stdout, "Google Chat message: https://example.com/message/123") {
		t.Fatalf("expected artifact ref, got %q", stdout)
	}
}

func TestPrintAgentResponsePrintsApprovalMetadata(t *testing.T) {
	stdout, _ := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Output: &contracts.UserOutput{
				Kind: contracts.UserOutputKindApproval,
				Text: "Can xac nhan truoc khi tiep tuc.",
				Meta: map[string]any{
					"approvalId": "appr_1",
					"expiresAt":  "2026-06-05T10:22:53+07:00",
				},
			},
		}, false, false)
	})

	if !strings.Contains(stdout, "Approval ID: appr_1") {
		t.Fatalf("expected approval id, got %q", stdout)
	}
	if !strings.Contains(stdout, "Expires At: 2026-06-05T10:22:53+07:00") {
		t.Fatalf("expected approval expiry, got %q", stdout)
	}
	if !strings.Contains(stdout, "Reply with: approve, reject, revise <comment>") {
		t.Fatalf("expected approval reply hint, got %q", stdout)
	}
}

func TestPrintAgentResponseRendersApprovalRequestWithoutOutput(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Status: contracts.AgentStatusApprovalRequired,
			ApprovalRequest: &contracts.ApprovalRequest{
				ApprovalID: "appr_1",
				Status:     contracts.ApprovalStatusPending,
				RiskLevel:  contracts.RiskLevelExternalWrite,
				Summary:    "Can gui tin nhan.",
				ToolCall: contracts.ToolCall{
					ToolName: "chat.sendMessage",
					Input: map[string]any{
						"text": "hello",
					},
				},
				ExpiresAt: time.Date(2026, 6, 5, 10, 22, 53, 0, time.FixedZone("ICT", 7*60*60)),
			},
		}, false, false)
	})

	for _, want := range []string{
		"Cần xác nhận trước khi thực hiện.",
		"Tool: chat.sendMessage",
		"Risk: external_write",
		"\"text\": \"hello\"",
		"Approval ID: appr_1",
		"Reply with: approve, reject, revise <comment>",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected stdout to contain %q, got %q", want, stdout)
		}
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestPrintAgentResponsePrintsErrorToStderr(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Status: contracts.AgentStatusFailed,
			Output: &contracts.UserOutput{
				Kind: contracts.UserOutputKindError,
				Text: "Khong the hoan tat yeu cau.",
			},
		}, false, false)
	})

	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("expected no stdout for error, got %q", stdout)
	}
	if !strings.Contains(stderr, "Khong the hoan tat yeu cau.") {
		t.Fatalf("expected stderr error output, got %q", stderr)
	}
}

func TestPrintAgentResponseFallsBackToMessage(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Message: "fallback message",
		}, false, false)
	})

	if !strings.Contains(stdout, "fallback message") {
		t.Fatalf("expected fallback message, got %q", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
}

func TestPrintAgentResponseTraceModeIncludesStatus(t *testing.T) {
	stdout, _ := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Status: contracts.AgentStatusCompleted,
			Output: &contracts.UserOutput{
				Kind: contracts.UserOutputKindSuccess,
				Text: "done",
			},
			ToolResults: []contracts.ToolResult{{ToolName: "chat.sendMessage", Success: true}},
		}, false, true)
	})

	if !strings.Contains(stdout, "done") {
		t.Fatalf("expected output text, got %q", stdout)
	}
	if !strings.Contains(stdout, "Status: completed") {
		t.Fatalf("expected trace status, got %q", stdout)
	}
	if !strings.Contains(stdout, "Tool results:") {
		t.Fatalf("expected trace tool results, got %q", stdout)
	}
}

func captureStdStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr

	var stdoutBuf bytes.Buffer
	_, _ = io.Copy(&stdoutBuf, stdoutR)
	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, stderrR)

	_ = stdoutR.Close()
	_ = stderrR.Close()

	return stdoutBuf.String(), stderrBuf.String()
}
