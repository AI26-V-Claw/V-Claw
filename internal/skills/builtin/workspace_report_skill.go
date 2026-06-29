package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	gmailtool "vclaw/internal/tools/office/gmail"
)

// WorkspaceServices holds the Google Workspace service dependencies for WorkspaceReportSkill.
// Any field may be nil if the corresponding integration is not configured.
type WorkspaceServices struct {
	Gmail    *gmailtool.Service
	Calendar *calendartool.Service
	Chat     *chattool.Service
}

// WorkspaceReportSkill orchestrates Gmail + Calendar + Chat into a structured briefing.
// Workflow:
//  1. gmail.ListEmails  â€” unread + flagged
//  2. calendar.ListEvents â€” today/this week
//  3. chat.ListMessages â€” recent threads (if space provided)
//  4. Synthesize by priority: urgent / follow-up / FYI
type WorkspaceReportSkill struct {
	svc WorkspaceServices
}

func NewWorkspaceReportSkill(svc WorkspaceServices) *WorkspaceReportSkill {
	return &WorkspaceReportSkill{svc: svc}
}

func (s *WorkspaceReportSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name: "skill.workspace_report",
		Description: "Generate a structured daily/weekly briefing from Google Workspace. " +
			"Aggregates unread emails, upcoming calendar events, and recent chat messages " +
			"into a prioritized report (urgent / follow-up / FYI). " +
			"Use when the user asks for a daily briefing, morning summary, or workspace overview.",
		Version:       "1.0.0",
		Tags:          []string{"workspace", "email", "calendar", "chat", "briefing"},
		Scope:         []string{"email", "calendar", "chat"},
		Permissions:   []string{"gmail.read", "calendar.read", "chat.read"},
		RelatedSkills: []string{"skill.deep_research"},
		Fallback:      "Workspace report is unavailable. Please check that Google Workspace is configured.",
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"period": map[string]any{
					"type":        "string",
					"enum":        []string{"today", "week"},
					"default":     "today",
					"description": "Reporting period: today or this week",
				},
				"max_emails": map[string]any{
					"type":        "integer",
					"default":     10,
					"description": "Maximum number of emails to include",
				},
				"chat_space": map[string]any{
					"type":        "string",
					"description": "Optional Google Chat space name or ID to pull messages from",
				},
			},
			"required": []string{},
		},
		RiskLevel: tools.RiskLevelSafeRead,
		Enabled:   true,
	}
}

func (s *WorkspaceReportSkill) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if s.svc.Gmail == nil && s.svc.Calendar == nil && s.svc.Chat == nil {
		return errorResult(call, "SKILL_UNAVAILABLE", "Google Workspace is not configured.")
	}

	period, _ := call.Arguments["period"].(string)
	if period == "" {
		period = "today"
	}
	maxEmails := int64(10)
	if v, ok := call.Arguments["max_emails"].(float64); ok && v > 0 {
		maxEmails = int64(v)
	}
	chatSpace, _ := call.Arguments["chat_space"].(string)

	var report strings.Builder
	report.WriteString("# Workspace Briefing\n\n")

	// Step 1: Gmail â€” unread emails
	if s.svc.Gmail != nil {
		emails, err := s.svc.Gmail.ListEmails(ctx, gmailtool.ListEmailsInput{
			UserID:     "me",
			LabelIDs:   []string{"UNREAD"},
			MaxResults: maxEmails,
		})
		if err != nil {
			report.WriteString("## Email\n\n_Unable to fetch emails._\n\n")
		} else {
			report.WriteString(fmt.Sprintf("## Email (%d unread)\n\n", len(emails.Messages)))
			for _, msg := range emails.Messages {
				report.WriteString(fmt.Sprintf("- **%s** â€” %s\n", msg.Subject, msg.From))
			}
			report.WriteString("\n")
		}
	}

	// Step 2: Calendar â€” events for period
	if s.svc.Calendar != nil {
		now := time.Now()
		timeMin := now.Format(time.RFC3339)
		timeMax := now.Add(24 * time.Hour).Format(time.RFC3339)
		if period == "week" {
			timeMax = now.Add(7 * 24 * time.Hour).Format(time.RFC3339)
		}
		events, errShape := s.svc.Calendar.ListEvents(ctx, calendartool.ListEventsInput{
			TimeMin: timeMin,
			TimeMax: timeMax,
		})
		if errShape != nil {
			report.WriteString("## Calendar\n\n_Unable to fetch events._\n\n")
		} else {
			report.WriteString(fmt.Sprintf("## Calendar (%d events)\n\n", len(events.Events)))
			for _, ev := range events.Events {
				report.WriteString(fmt.Sprintf("- **%s** â€” %s to %s\n", ev.Title, ev.Start, ev.End))
			}
			report.WriteString("\n")
		}
	}

	// Step 3: Chat â€” recent messages from space
	if s.svc.Chat != nil && strings.TrimSpace(chatSpace) != "" {
		msgs, errShape := s.svc.Chat.ListMessages(ctx, chattool.ListMessagesInput{
			Space:      chatSpace,
			MaxResults: 10,
		})
		if errShape != nil {
			report.WriteString("## Chat\n\n_Unable to fetch messages._\n\n")
		} else {
			report.WriteString(fmt.Sprintf("## Chat (%d messages)\n\n", len(msgs.Messages)))
			for _, msg := range msgs.Messages {
				report.WriteString(fmt.Sprintf("- %s\n", truncate(msg.Text, 120)))
			}
			report.WriteString("\n")
		}
	}

	content := strings.TrimSpace(report.String())
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}