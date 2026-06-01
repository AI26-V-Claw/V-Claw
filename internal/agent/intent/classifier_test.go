package intent

import (
	"context"
	"testing"
)

func TestClassify_Greeting(t *testing.T) {
	c := NewClassifier(DefaultConfig)

	cases := []struct {
		name  string
		input string
	}{
		{"Vietnamese greeting", "Chào buổi sáng"},
		{"English greeting", "Hello"},
		{"Thank you", "Cảm ơn bạn rất nhiều"},
		{"Bye", "Tạm biệt nhé"},
		{"Mixed", "Hey, xin chào!"},
		{"English thanks", "Thank you so much!"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Classify(context.Background(), c, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Intent.Type != TypeGreeting {
				t.Errorf("expected GREETING, got %s for %q", out.Intent.Type, tc.input)
			}
			if out.NeedsClarification {
				t.Errorf("greeting should not need clarification")
			}
			if len(out.Intent.ToolCalls) > 0 {
				t.Errorf("greeting should have no tool calls")
			}
		})
	}
}

func TestClassify_ReadInfo(t *testing.T) {
	c := NewClassifier(DefaultConfig)

	cases := []struct {
		name string
		input string
	}{
		{"Read file", "Đọc file config.json trong thư mục /etc"},
		{"Check mail", "Check mail xem có ai gửi báo cáo không"},
		{"Calendar", "Lịch họp ngày mai có gì không?"},
		{"Search", "Tìm kiếm báo cáo tài chính quý 3"},
		{"List dir", "Cho tôi xem danh sách file trong thư mục /var/log"},
		{"Open file", "Mở file document.pdf"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Classify(context.Background(), c, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Intent.Type != TypeReadInfo {
				t.Errorf("expected READ_INFO, got %s for %q", out.Intent.Type, tc.input)
			}
			if out.NeedsClarification {
				t.Errorf("clear read request should not need clarification")
			}
		})
	}
}

func TestClassify_DangerousAction_WithParams(t *testing.T) {
	c := NewClassifier(DefaultConfig)

	cases := []struct {
		name  string
		input string
	}{
		{"Delete with path", "Xóa file /tmp/test.log"},
		{"Send email complete", "Gửi email cho minh@example.com nội dung là 'Dự án đã xong'"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Classify(context.Background(), c, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Intent.Type != TypeDangerousAction {
				t.Errorf("expected DANGEROUS_ACTION, got %s for %q", out.Intent.Type, tc.input)
			}
			if !out.Intent.NeedsConfirm {
				t.Errorf("dangerous action should need confirmation")
			}
		})
	}
}

func TestClassify_DangerousAction_MissingParams(t *testing.T) {
	c := NewClassifier(DefaultConfig)

	cases := []struct {
		name  string
		input string
	}{
		{"Delete no path", "Xóa file giúp tôi"},
		{"Send no details", "Gửi email"},
		{"Delete vague", "Xóa file cấu hình đi"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Classify(context.Background(), c, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Intent.Type != TypeDangerousAction {
				t.Errorf("expected DANGEROUS_ACTION, got %s for %q", out.Intent.Type, tc.input)
			}
			if !out.NeedsClarification {
				t.Errorf("dangerous action with missing params MUST need clarification for %q", tc.input)
			}
			if out.ClarificationMessage == "" && out.ClarificationOptions == nil {
				t.Errorf("must have either clarification message or options for %q", tc.input)
			}
		})
	}
}

func TestClassify_Composite(t *testing.T) {
	c := NewClassifier(DefaultConfig)

	cases := []struct {
		name  string
		input string
	}{
		{"Find and delete", "Tìm và xóa các file log cũ hơn 30 ngày"},
		{"Find then delete", "Tìm file log rồi xóa đi"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Classify(context.Background(), c, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Intent.Type != TypeComposite {
				t.Errorf("expected COMPOSITE_ACTION, got %s for %q", out.Intent.Type, tc.input)
			}
			if !out.Intent.NeedsConfirm {
				t.Errorf("composite action should need confirmation")
			}
		})
	}
}

func TestClassify_Unknown(t *testing.T) {
	c := NewClassifier(DefaultConfig)

	cases := []struct {
		name  string
		input string
	}{
		{"Vague reference", "Làm như hôm qua"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Classify(context.Background(), c, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !out.NeedsClarification {
				t.Errorf("unknown/vague input should need clarification for %q", tc.input)
			}
		})
	}
}

func TestClassify_EmptyInput(t *testing.T) {
	c := NewClassifier(DefaultConfig)
	_, err := Classify(context.Background(), c, "")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestValidate_BlocksDangerousWithMissingParams(t *testing.T) {
	result := &Result{
		Type:          TypeDangerousAction,
		Confidence:    0.95,
		MissingParams: []string{"path"},
		ToolCalls: []ToolCallInfo{
			{Name: "delete_file", Category: "DANGEROUS_WRITE"},
		},
	}

	out := Validate(result, DefaultConfig)
	if !out.NeedsClarification {
		t.Error("validator must block dangerous action with missing params")
	}
	if out.ClarificationMessage == "" {
		t.Error("validator must provide clarification message")
	}
}

func TestValidate_AllowsSafeRead(t *testing.T) {
	result := &Result{
		Type:       TypeReadInfo,
		Confidence: 0.85,
		ToolCalls: []ToolCallInfo{
			{Name: "read_file", Category: "SAFE_READ"},
		},
	}

	out := Validate(result, DefaultConfig)
	if out.NeedsClarification {
		t.Error("safe read with good confidence should pass through")
	}
}
