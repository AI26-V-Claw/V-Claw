package agent

import (
	"strings"
	"testing"

	"vclaw/internal/tools"
)

func TestBuildTaskPlannerSystemPromptUsesXMLSections(t *testing.T) {
	prompt := BuildTaskPlannerSystemPrompt([]tools.ToolDefinition{{
		Name:             "calendar.createEvent",
		Description:      "Create calendar event",
		RiskLevel:        tools.RiskLevelExternalWrite,
		RequiresApproval: true,
		Enabled:          true,
	}})

	for _, want := range []string{
		"<task_planner_system_prompt>",
		"<persona>",
		"<rules>",
		"<tools_instruction>",
		"<response_format>",
		"<constraints>",
		"assistant vừa hỏi làm rõ",
		"approval trước khi thực thi",
		"required_fields",
		"calendar.createEvent, required_fields là title, start, end",
		"Location là optional",
		"people.searchDirectory",
		"attendees phải là email hợp lệ",
		"Attachment paths:",
		"gmail.downloadAttachments",
		"file người dùng vừa gửi qua Telegram/Slack",
		`<tool name="calendar.createEvent"`,
		`requiresApproval="true"`,
		`required_fields=""`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected planner prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}

func TestTaskPlannerToolInstructionsIncludesRequiredFields(t *testing.T) {
	instructions := taskPlannerToolInstructions([]tools.ToolDefinition{{
		Name:             "calendar.createEvent",
		Description:      "Create calendar event",
		RiskLevel:        tools.RiskLevelExternalWrite,
		RequiresApproval: true,
		Enabled:          true,
		Parameters: tools.ToolSchema{
			"required": []string{"title", "start", "end"},
		},
	}})

	if !strings.Contains(instructions, `required_fields="title,start,end"`) {
		t.Fatalf("expected required fields in tool instructions, got:\n%s", instructions)
	}
}

func TestBuildTaskPlannerUserPromptIncludesRecentHistory(t *testing.T) {
	prompt := BuildTaskPlannerUserPrompt(TaskPlanningInput{
		RecentHistory: []string{
			"user: Tao lich hop voi Bao ngay mai luc 10am",
			"assistant: Thoi gian ket thuc la khi nao?",
		},
	})

	for _, want := range []string{
		"<recent_history>",
		"Tao lich hop voi Bao",
		"Thoi gian ket thuc",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected planner user prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}

func TestNormalizeTaskPlanLimitsAndFillsSteps(t *testing.T) {
	wire := taskPlannerWireResult{}
	for i := 0; i < 10; i++ {
		wire.Steps = append(wire.Steps, struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			ToolName    string `json:"tool_name"`
			Status      string `json:"status"`
		}{
			ToolName: "gmail.listEmails",
		})
	}

	result := normalizeTaskPlan(wire)
	if len(result.Plan.Steps) != 6 {
		t.Fatalf("expected plan to be limited to 6 steps, got %d", len(result.Plan.Steps))
	}
	if result.Plan.Steps[0].ID != "step_1" {
		t.Fatalf("expected generated step id, got %#v", result.Plan.Steps[0])
	}
	if !strings.Contains(result.Plan.Steps[0].Description, "gmail.listEmails") {
		t.Fatalf("expected tool name in generated description, got %#v", result.Plan.Steps[0])
	}
}
