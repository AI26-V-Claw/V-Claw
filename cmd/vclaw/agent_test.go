package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"vclaw/internal/contracts"
)

type blockingChatMessenger struct {
	started   chan struct{}
	release   chan struct{}
	cancelled chan string
	onStarted sync.Once
	response  contracts.AgentResponse
}

func newBlockingChatMessenger() *blockingChatMessenger {
	return &blockingChatMessenger{
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		cancelled: make(chan string, 1),
	}
}

func (m *blockingChatMessenger) HandleMessage(ctx context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error) {
	m.onStarted.Do(func() { close(m.started) })
	select {
	case <-m.release:
		if m.response.Status != "" || m.response.Message != "" || m.response.Output != nil {
			return m.response, nil
		}
		return contracts.AgentResponse{Status: contracts.AgentStatusCompleted, Message: "done"}, nil
	case <-ctx.Done():
		return contracts.AgentResponse{Status: contracts.AgentStatusCancelled}, nil
	}
}

func (m *blockingChatMessenger) CancelSession(sessionID string) bool {
	m.cancelled <- sessionID
	return true
}

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

func TestPrintAgentResponsePrintsExitReasonWithIterationCount(t *testing.T) {
	_, stderr := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Status: contracts.AgentStatusIterationBudgetExhausted,
			Data: map[string]any{
				"iteration_used":  8,
				"iteration_limit": 8,
			},
		}, false, false)
	})

	for _, want := range []string{"[exit]", "8/8", "ngân sách xử lý"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("expected stderr to contain %q, got %q", want, stderr)
		}
	}
}

func TestPrintAgentResponsePrintsCurrentPlanStep(t *testing.T) {
	_, stderr := captureStdStreams(t, func() {
		printAgentResponse(contracts.AgentResponse{
			Status: contracts.AgentStatusApprovalRequired,
			Plan: &contracts.Plan{Steps: []contracts.PlanStep{
				{Description: "Đọc email gần đây", Status: "completed"},
				{Description: "Tạo draft trả lời", Status: "in_progress"},
				{Description: "Xin xác nhận gửi", Status: "pending"},
			}},
		}, false, false)
	})

	for _, want := range []string{"[plan]", "✓ Đọc email gần đây", "▶ Tạo draft trả lời", "· Xin xác nhận gửi"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("expected stderr to contain %q, got %q", want, stderr)
		}
	}
}

func TestRunAgentChatLoopProcessesStopWhileMessageIsRunning(t *testing.T) {
	messenger := newBlockingChatMessenger()
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe input: %v", err)
	}
	defer inputR.Close()

	output := &bytes.Buffer{}
	done := make(chan error, 1)
	sessionID := "dev"
	go func() {
		done <- runAgentChatLoop(context.Background(), inputR, output, messenger, &sessionID, "dev-cli", false, false, time.Now)
	}()

	if _, err := inputW.WriteString("long request\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	select {
	case <-messenger.started:
	case <-time.After(time.Second):
		t.Fatal("HandleMessage did not start")
	}
	if _, err := inputW.WriteString("/stop\n/exit\n"); err != nil {
		t.Fatalf("write stop: %v", err)
	}

	select {
	case got := <-messenger.cancelled:
		if got != "dev" {
			t.Fatalf("CancelSession() session = %q, want dev", got)
		}
	case <-time.After(time.Second):
		t.Fatal("/stop was not processed while HandleMessage was running")
	}
	close(messenger.release)
	_ = inputW.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runAgentChatLoop() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("chat loop did not exit")
	}
}

func TestRunAgentChatLoopPrintsResponseWithoutMoreInput(t *testing.T) {
	messenger := newBlockingChatMessenger()
	messenger.response = contracts.AgentResponse{Status: contracts.AgentStatusCompleted, Message: "visible response"}
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe input: %v", err)
	}
	defer inputR.Close()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	defer stdoutR.Close()
	origStdout := os.Stdout
	os.Stdout = stdoutW
	t.Cleanup(func() { os.Stdout = origStdout })

	output := &bytes.Buffer{}
	done := make(chan error, 1)
	sessionID := "dev"
	go func() {
		done <- runAgentChatLoop(context.Background(), inputR, output, messenger, &sessionID, "dev-cli", false, false, time.Now)
	}()

	if _, err := inputW.WriteString("hello\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	select {
	case <-messenger.started:
	case <-time.After(time.Second):
		t.Fatal("HandleMessage did not start")
	}
	close(messenger.release)

	stdoutText := make(chan string, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 && strings.Contains(string(buf[:n]), "visible response") {
				stdoutText <- string(buf[:n])
				return
			}
			if err != nil {
				stdoutText <- ""
				return
			}
		}
	}()
	select {
	case got := <-stdoutText:
		if !strings.Contains(got, "visible response") {
			t.Fatalf("response was not printed without more input; stdout chunk = %q", got)
		}
	case <-time.After(time.Second):
		_ = stdoutW.Close()
		os.Stdout = origStdout
		t.Fatalf("response was not printed without more input; prompt output = %q", output.String())
	}
	os.Stdout = origStdout
	_ = stdoutW.Close()

	deadline := time.After(time.Second)
	for !strings.Contains(output.String(), "You>") {
		select {
		case <-deadline:
			t.Fatalf("prompt was not printed; output = %q", output.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if _, err := inputW.WriteString("/exit\n"); err != nil {
		t.Fatalf("write exit: %v", err)
	}
	_ = inputW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runAgentChatLoop() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("chat loop did not exit")
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
