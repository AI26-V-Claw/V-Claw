package risk

import (
	"testing"

	"vclaw/internal/agent/intent"
)

func TestClassifier_Assess(t *testing.T) {
	classifier := NewClassifier()

	tests := []struct {
		name             string
		toolName         string
		intentType       intent.IntentType
		expectedRisk     Level
		expectedDecision Decision
		expectedApproval bool
	}{
		{
			name:             "Safe read - gmail.listEmails",
			toolName:         "gmail.listEmails",
			intentType:       intent.TypeReadInfo,
			expectedRisk:     SafeRead,
			expectedDecision: Allow,
			expectedApproval: false,
		},
		{
			name:             "Safe read - gmail.listDrafts",
			toolName:         "gmail.listDrafts",
			intentType:       intent.TypeReadInfo,
			expectedRisk:     SafeRead,
			expectedDecision: Allow,
			expectedApproval: false,
		},
		{
			name:             "Safe read - calendar.listEvents",
			toolName:         "calendar.listEvents",
			intentType:       intent.TypeReadInfo,
			expectedRisk:     SafeRead,
			expectedDecision: Allow,
			expectedApproval: false,
		},
		{
			name:             "Safe read - chat.findSpacesByMembers",
			toolName:         "chat.findSpacesByMembers",
			intentType:       intent.TypeReadInfo,
			expectedRisk:     SafeRead,
			expectedDecision: Allow,
			expectedApproval: false,
		},
		{
			name:             "Safe read - people.searchDirectory",
			toolName:         "people.searchDirectory",
			intentType:       intent.TypeReadInfo,
			expectedRisk:     SafeRead,
			expectedDecision: Allow,
			expectedApproval: false,
		},
		{
			name:             "External write - gmail.createDraft",
			toolName:         "gmail.createDraft",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     ExternalWrite,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "External write - gmail.batchModifyMessages",
			toolName:         "gmail.batchModifyMessages",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     ExternalWrite,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "External write - gmail.untrashMessage",
			toolName:         "gmail.untrashMessage",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     ExternalWrite,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Local write - gmail.downloadAttachments",
			toolName:         "gmail.downloadAttachments",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     LocalWrite,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Destructive - gmail.deleteDraft",
			toolName:         "gmail.deleteDraft",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     Destructive,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Destructive - gmail.trashMessage",
			toolName:         "gmail.trashMessage",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     Destructive,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Destructive - calendar.deleteEvent",
			toolName:         "calendar.deleteEvent",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     Destructive,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "External write - chat.updateMessage",
			toolName:         "chat.updateMessage",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     ExternalWrite,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Destructive - chat.deleteMessage",
			toolName:         "chat.deleteMessage",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     Destructive,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Code execution - sandbox.runPython",
			toolName:         "sandbox.runPython",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     CodeExecution,
			expectedDecision: RequiresApproval,
			expectedApproval: true,
		},
		{
			name:             "Decision block - format_disk",
			toolName:         "format_disk",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     Destructive,
			expectedDecision: Block,
			expectedApproval: false,
		},
		{
			name:             "Unknown tool - should block",
			toolName:         "unknown_tool",
			intentType:       intent.TypeDangerousAction,
			expectedRisk:     Destructive,
			expectedDecision: Block,
			expectedApproval: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assessment, err := classifier.Assess(tt.toolName, tt.intentType)
			if err != nil {
				t.Fatalf("Assess() error = %v", err)
			}

			if assessment.RiskLevel != tt.expectedRisk {
				t.Errorf("RiskLevel = %v, want %v", assessment.RiskLevel, tt.expectedRisk)
			}

			if assessment.Decision != tt.expectedDecision {
				t.Errorf("Decision = %v, want %v", assessment.Decision, tt.expectedDecision)
			}

			if assessment.RequiresApproval != tt.expectedApproval {
				t.Errorf("RequiresApproval = %v, want %v", assessment.RequiresApproval, tt.expectedApproval)
			}

			if assessment.ReasonVi == "" {
				t.Error("ReasonVi should not be empty")
			}
		})
	}
}

func TestClassifier_UpdatePolicy(t *testing.T) {
	classifier := NewClassifier()

	// Initially, custom_tool is unknown and blocked with a valid contract risk level.
	assessment, _ := classifier.Assess("custom_tool", intent.TypeDangerousAction)
	if assessment.RiskLevel != Destructive {
		t.Errorf("Expected Destructive for unknown tool, got %v", assessment.RiskLevel)
	}
	if assessment.Decision != Block {
		t.Errorf("Expected Block decision for unknown tool, got %v", assessment.Decision)
	}

	// Update policy
	classifier.UpdatePolicy("custom_tool", SafeRead)

	// Now it should be safe
	assessment, _ = classifier.Assess("custom_tool", intent.TypeReadInfo)
	if assessment.RiskLevel != SafeRead {
		t.Errorf("Expected SafeRead after update, got %v", assessment.RiskLevel)
	}
	if assessment.Decision != Allow {
		t.Errorf("Expected Allow after update, got %v", assessment.Decision)
	}
}

func TestClassifier_GetPolicy(t *testing.T) {
	classifier := NewClassifier()

	// Test known tool
	level, ok := classifier.GetPolicy("gmail.listEmails")
	if !ok {
		t.Error("Expected gmail.listEmails to be in policy")
	}
	if level != SafeRead {
		t.Errorf("Expected SafeRead for gmail.listEmails, got %v", level)
	}

	// Test unknown tool
	_, ok = classifier.GetPolicy("unknown_tool")
	if ok {
		t.Error("Expected unknown_tool to not be in policy")
	}
}

func TestRiskLevelCoverage(t *testing.T) {
	classifier := NewClassifier()

	// Ensure all risk levels are covered in the policy
	riskLevels := map[Level]bool{
		SafeRead:      false,
		SafeCompute:   false,
		ExternalWrite: false,
		LocalWrite:    false,
		CodeExecution: false,
		Destructive:   false,
	}

	for _, level := range classifier.policy {
		riskLevels[level] = true
	}

	// Check that we have at least one tool for each major risk level
	// (SafeCompute may not have tools in the default policy)
	requiredLevels := []Level{SafeRead, ExternalWrite, LocalWrite, CodeExecution, Destructive}
	for _, level := range requiredLevels {
		if !riskLevels[level] {
			t.Errorf("No tools found for risk level: %v", level)
		}
	}
}
