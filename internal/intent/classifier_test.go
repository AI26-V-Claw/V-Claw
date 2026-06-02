package intent

import "testing"

func TestClassifierRules(t *testing.T) {
	classifier := NewClassifier()

	cases := []struct {
		name     string
		input    string
		intent   Intent
		systemOp SystemOpType
	}{
		{name: "greeting", input: "xin chào", intent: IntentGreeting, systemOp: SystemOpNone},
		{name: "greeting chao", input: "chào V-Claw", intent: IntentGreeting, systemOp: SystemOpNone},
		{name: "greeting morning", input: "chào buổi sáng", intent: IntentGreeting, systemOp: SystemOpNone},
		{name: "greeting hi token", input: "hi bot", intent: IntentGreeting, systemOp: SystemOpNone},
		{name: "read info", input: "hôm nay tôi có lịch gì", intent: IntentReadInfo, systemOp: SystemOpNone},
		{name: "read info not greeting", input: "xem lịch họp chiều nay", intent: IntentReadInfo, systemOp: SystemOpNone},
		{name: "send", input: "gửi email cho Nam", intent: IntentSystemOp, systemOp: SystemOpSend},
		{name: "write not greeting", input: "tạo ghi chú cuộc họp", intent: IntentSystemOp, systemOp: SystemOpWrite},
		{name: "write not greeting 2", input: "ghi file báo cáo tổng kết", intent: IntentSystemOp, systemOp: SystemOpWrite},
		{name: "ambiguous", input: "làm đi", intent: IntentAmbiguous, systemOp: SystemOpNone},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifier.Classify(testCase.input)
			if result.Intent != testCase.intent {
				t.Fatalf("unexpected intent: got %s want %s", result.Intent, testCase.intent)
			}
			if result.SystemOpType != testCase.systemOp {
				t.Fatalf("unexpected system op: got %s want %s", result.SystemOpType, testCase.systemOp)
			}
			if testCase.intent == IntentAmbiguous && result.ClarifyQuestion == "" {
				t.Fatal("expected clarify question for ambiguous intent")
			}
		})
	}
}

func TestClassifierDoesNotTreatEmbeddedHiAsGreeting(t *testing.T) {
	classifier := NewClassifier()

	cases := []struct {
		name  string
		input string
	}{
		{name: "chiều", input: "xem lịch họp chiều nay"},
		{name: "ghi note", input: "tạo ghi chú cuộc họp"},
		{name: "ghi file", input: "ghi file báo cáo tổng kết"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifier.Classify(testCase.input)
			if result.Intent == IntentGreeting {
				t.Fatalf("unexpected greeting classification for %q", testCase.input)
			}
		})
	}
}
