package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	agentintent "vclaw/internal/agent/intent"
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

type TaskPlanner interface {
	Plan(ctx context.Context, input TaskPlanningInput) (*TaskPlanResult, error)
}

type TaskPlanningInput struct {
	Message        contracts.UserMessage
	Classification *agentintent.ClassificationOutput
	Tools          []tools.ToolDefinition
	RecentHistory  []string
	Now            time.Time
}

type TaskPlanResult struct {
	Plan                 contracts.Plan
	NeedsClarification   bool
	ClarificationMessage string
}

type LLMTaskPlanner struct {
	provider providers.Provider
	model    string
}

func NewLLMTaskPlanner(provider providers.Provider, model string) *LLMTaskPlanner {
	return &LLMTaskPlanner{provider: provider, model: strings.TrimSpace(model)}
}

func (p *LLMTaskPlanner) Plan(ctx context.Context, input TaskPlanningInput) (*TaskPlanResult, error) {
	if p == nil || p.provider == nil {
		return nil, fmt.Errorf("task planner provider is required")
	}
	req := &providers.GenerateRequest{
		SystemPrompt:   BuildTaskPlannerSystemPrompt(input.Tools),
		UserPrompt:     BuildTaskPlannerUserPrompt(input),
		Temperature:    0.1,
		MaxTokens:      2048,
		ResponseFormat: "json",
		Model:          p.model,
	}
	resp, err := p.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("task planning failed: %w", err)
	}
	var wire taskPlannerWireResult
	if err := json.Unmarshal([]byte(extractPlannerJSONObject(resp.Text)), &wire); err != nil {
		return nil, fmt.Errorf("parse task planner response: %w", err)
	}
	return normalizeTaskPlan(wire), nil
}

type taskPlannerWireResult struct {
	Steps []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		ToolName    string `json:"tool_name"`
		Status      string `json:"status"`
	} `json:"steps"`
	NeedsClarification   bool   `json:"needs_clarification"`
	ClarificationMessage string `json:"clarification_message"`
}

func BuildTaskPlannerSystemPrompt(toolDefs []tools.ToolDefinition) string {
	return strings.TrimSpace(fmt.Sprintf(`<task_planner_system_prompt>
  <persona>
    <identity>Bạn là Task Planner của V-Claw, một local-first personal AI agent assistant.</identity>
    <mission>Lập kế hoạch các bước và tool cần dùng trước khi Agent Core gọi tool.</mission>
    <language>Luôn viết mô tả bước và câu hỏi làm rõ bằng tiếng Việt ngắn gọn.</language>
  </persona>

  <rules>
    <rule>Chỉ lập kế hoạch, không thực thi tool và không nói hành động đã hoàn tất.</rule>
    <rule>Chỉ chọn tool có trong tools_instruction.</rule>
    <rule>Safe-read tool có thể nằm trong plan mà không cần approval.</rule>
    <rule>Tool có requiresApproval=true vẫn được đưa vào plan, nhưng phải ghi rõ đây là bước cần approval trước khi chạy.</rule>
    <rule>Nếu thiếu thông tin bắt buộc để lập kế hoạch an toàn, đặt needs_clarification=true và hỏi đúng một câu ngắn.</rule>
    <rule>Nếu intent là chào hỏi hoặc không cần tool, trả về steps rỗng.</rule>
    <rule>Không tự lấy tham số từ bộ nhớ cũ cho hành động write/destructive.</rule>
    <rule>Nếu recent_history cho thấy assistant vừa hỏi làm rõ và user hiện tại trả lời trực tiếp câu hỏi đó, hãy dùng ngữ cảnh đang mở này để hoàn thiện plan.</rule>
    <rule>Ngữ cảnh đang mở chỉ giúp hoàn thiện plan; tool write/destructive vẫn phải đi qua approval trước khi thực thi.</rule>
    <rule>Chỉ hỏi lại khi thiếu field bắt buộc trong required_fields hoặc thiếu thông tin safety-critical; không hỏi chỉ vì thiếu field optional.</rule>
    <rule>Với calendar.createEvent, required_fields là title, start, end. Nếu thiếu end nhưng có start, hãy hỏi giờ kết thúc hoặc thời lượng. Location là optional; nếu thiếu location thì để trống và tiếp tục.</rule>
    <rule>Với calendar.createEvent/calendar.updateEvent, attendees phải là email hợp lệ. Nếu user dùng tên người như Bao hoặc Tung, plan phải thêm people.searchDirectory trước để resolve email Workspace.</rule>
    <rule>Nếu user trả lời "không", "khong", "no" cho một câu hỏi optional như location, xem như optional field đó bị bỏ qua và tiếp tục plan.</rule>
    <rule>Nếu user_message hoặc recent_history có "Attachment paths:", đó là file local người dùng vừa gửi qua channel hiện tại. Khi user nói "file này", "file tôi đã gửi", "ảnh này", hoặc yêu cầu đính kèm/gửi/upload file hiện tại, dùng các path đó trong input attachments của tool phù hợp.</rule>
    <rule>Không dùng gmail.downloadAttachments cho file người dùng vừa gửi qua Telegram/Slack. Chỉ dùng gmail.downloadAttachments khi user yêu cầu tải attachment từ một Gmail message có messageId.</rule>
    <rule>Vá»›i Google Chat, khÃ´ng láº­p plan gá»i chat.sendMessage/chat.listMessages báº±ng tÃªn nhÃ³m, tÃªn ngÆ°á»i, email, hoáº·c display name trong field space. Field space pháº£i lÃ  resource name dáº¡ng spaces/AAAA. Náº¿u user chá»‰ nÃ³i tÃªn nhÃ³m/ngÆ°á»i, plan pháº£i resolve trÆ°á»›c báº±ng people.searchDirectory + chat.findSpacesByMembers hoáº·c chat.listSpaces.</rule>
  </rules>

  <tools_instruction>
%s
  </tools_instruction>

  <response_format>
    <format>Chỉ trả về một JSON object hợp lệ. Không dùng Markdown, không thêm giải thích ngoài JSON.</format>
    <schema>
      {
        "steps": [
          {
            "id": "step_1",
            "description": "Mô tả bước bằng tiếng Việt, gồm tên tool nếu có",
            "tool_name": "tool.name",
            "status": "pending"
          }
        ],
        "needs_clarification": false,
        "clarification_message": ""
      }
    </schema>
  </response_format>

  <constraints>
    <constraint>Không đưa secret, token, chat_id, user_id, hoặc dữ liệu nhạy cảm vào plan.</constraint>
    <constraint>Plan chỉ là định hướng; policy runtime vẫn là nguồn quyết định approval cuối cùng.</constraint>
    <constraint>Nếu tool write/destructive thiếu recipient/time/path/content/target, phải hỏi lại thay vì lập plan thực thi.</constraint>
    <constraint>Giữ plan tối đa 6 bước, ưu tiên bước cần thiết nhất.</constraint>
  </constraints>
</task_planner_system_prompt>`, taskPlannerToolInstructions(toolDefs)))
}

func BuildTaskPlannerUserPrompt(input TaskPlanningInput) string {
	intentType := ""
	needsClarification := false
	missingParams := ""
	if input.Classification != nil && input.Classification.Intent != nil {
		intentType = string(input.Classification.Intent.Type)
		needsClarification = input.Classification.NeedsClarification
		missingParams = strings.Join(input.Classification.Intent.MissingParams, ", ")
	}
	return strings.TrimSpace(fmt.Sprintf(`<task_planning_request>
  <current_time>%s</current_time>
  <channel>%s</channel>
  <user_message>%s</user_message>
  <recent_history>%s</recent_history>
  <intent_context>
    <intent_type>%s</intent_type>
    <needs_clarification>%t</needs_clarification>
    <missing_params>%s</missing_params>
  </intent_context>
</task_planning_request>`,
		input.Now.Format(time.RFC3339),
		xmlEscape(input.Message.Channel),
		xmlEscape(input.Message.Text),
		xmlEscape(strings.Join(input.RecentHistory, "\n")),
		xmlEscape(intentType),
		needsClarification,
		xmlEscape(missingParams),
	))
}

func taskPlannerToolInstructions(toolDefs []tools.ToolDefinition) string {
	lines := make([]string, 0, len(toolDefs))
	for _, toolDef := range toolDefs {
		if !toolDef.Enabled {
			continue
		}
		lines = append(lines, fmt.Sprintf(`    <tool name="%s" riskLevel="%s" requiresApproval="%t" required_fields="%s">%s</tool>`,
			xmlEscape(toolDef.Name),
			xmlEscape(string(toolDef.RiskLevel)),
			toolDef.RequiresApproval,
			xmlEscape(strings.Join(requiredFieldsFromSchema(toolDef.Parameters), ",")),
			xmlEscape(toolDef.Description),
		))
	}
	return strings.Join(lines, "\n")
}

func requiredFieldsFromSchema(schema tools.ToolSchema) []string {
	required, ok := schema["required"]
	if !ok {
		return nil
	}
	switch values := required.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		fields := make([]string, 0, len(values))
		for _, value := range values {
			field := strings.TrimSpace(fmt.Sprint(value))
			if field != "" {
				fields = append(fields, field)
			}
		}
		return fields
	default:
		return nil
	}
}

func normalizeTaskPlan(wire taskPlannerWireResult) *TaskPlanResult {
	result := &TaskPlanResult{
		NeedsClarification:   wire.NeedsClarification,
		ClarificationMessage: strings.TrimSpace(wire.ClarificationMessage),
	}
	for index, step := range wire.Steps {
		description := strings.TrimSpace(step.Description)
		toolName := strings.TrimSpace(step.ToolName)
		if description == "" && toolName != "" {
			description = "Dùng tool " + toolName + "."
		}
		if description == "" {
			continue
		}
		if toolName != "" && !strings.Contains(description, toolName) {
			description = toolName + ": " + description
		}
		id := strings.TrimSpace(step.ID)
		if id == "" {
			id = fmt.Sprintf("step_%d", index+1)
		}
		status := strings.TrimSpace(step.Status)
		if status == "" {
			status = "pending"
		}
		result.Plan.Steps = append(result.Plan.Steps, contracts.PlanStep{
			ID:          id,
			Description: description,
			Status:      status,
		})
		if len(result.Plan.Steps) >= 6 {
			break
		}
	}
	return result
}

func extractPlannerJSONObject(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func xmlEscape(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}
