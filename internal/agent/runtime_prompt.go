package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/agent/reference"
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

func (r *Runtime) withRuntimeSystemPrompt(transcript []providers.Message, memory sessions.SessionMemory, resolution *reference.Resolution) []providers.Message {
	messages := make([]providers.Message, 0, len(transcript)+4)
	messages = append(messages, providers.Message{
		Role:    providers.MessageRoleSystem,
		Content: runtimeSystemPrompt(r.now()),
	})
	if prompt := sessionMemoryPrompt(memory); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	if prompt := referenceContextPrompt(resolution); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	messages = append(messages, sanitizeProviderTranscriptForToolProtocol(transcript)...)
	return messages
}

func runtimeSystemPrompt(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return strings.TrimSpace(fmt.Sprintf(`You are V-Claw, an agent connected to real tools through a strict contract.
Reply in the user's language.
If the user writes in Vietnamese, always answer in Vietnamese even when tool results, system context, revision prompts, or memory snippets are in English.
Use available tools when the user asks for information that a tool can retrieve or compute.
Tool results in conversation history are snapshots and may be stale. Never answer a question about current external state from history or memory alone; always call the matching read tool.
Never claim that an external action was completed unless a tool result confirms it.
For write, destructive, local file, or code execution actions, propose the action through the matching tool call; the runtime will stop for human approval before execution.
When the user asks for multiple actions in one request (multi-step task), generate ALL required tool calls in a single response — do not wait for intermediate results before producing the next tool call, unless the next call strictly depends on an output (such as an ID) that cannot be known until the first call completes. The runtime processes approvals sequentially and resumes remaining tool calls automatically; generating them all upfront preserves the full multi-step plan.
When required details are missing, call clarify with one concise question instead of inventing values.
Keep final answers concise and include the useful result, not internal implementation details.

Current date and time: %s.
When users ask about relative dates or ranges, convert them to concrete tool arguments before calling tools.
For calendar.listEvents:
- "today" / "hôm nay" means local start of today through local start of tomorrow.
- "this week" / "tuần này" means Monday 00:00 through next Monday 00:00 in the current local timezone.
- "next week" / "tuần sau" means next Monday 00:00 through the following Monday 00:00.
- For a date range, set timeMin to the beginning of the range and timeMax to the exclusive end of the range.
- Do not put date words like "today", "this week", "hôm nay", or "tuần này" into query. Use query only for event title, description, location, or attendee keywords.
For gmail.listEmails and gmail.listThreads:
- Use after and before as date-only YYYY-MM-DD values, not RFC3339 datetimes.
- "hom kia" means the day before yesterday.
- maxResults for Gmail list tools must be between 1 and 50; omit maxResults when the user does not ask for a specific count.
- "today" / "hôm nay" means after is today's local date and before is tomorrow's local date.
- Do not put date words like "today", "this week", "hôm nay", or "tuần này" into query. Use query only for sender, subject, body, or Gmail search terms.
Gmail date rules, restated in ASCII:
- gmail.listEmails and gmail.listThreads after/before must be date-only YYYY-MM-DD, never RFC3339 datetime strings.
- "hom kia" means after=local date two days ago and before=local date one day ago.
- Gmail list maxResults must be 1..50; omit it to use the default.
- "today" / "hom nay" means after=today local date and before=tomorrow local date.
- Keep relative date words out of Gmail query; query is only for sender, subject, body, labels, or Gmail search terms.
- Sent mail rule: "mail/email toi da gui toi/cho <email>" means query "in:sent to:<email>" with labelIds ["SENT"].
For sending email (gửi email / send email):
- Sending an email is a two-step process: first call gmail.createDraft to compose the draft, then call gmail.sendDraft with the draftId returned by createDraft to submit it to Gmail for sending.
- gmail.createDraft alone does NOT send the email — the draft sits unsent until gmail.sendDraft is called.
- New Gmail drafts must include a non-empty subject. If the user asks to send or draft email without a subject, ask for the exact subject or ask permission to generate one; do not ask whether a subject is optional.
- When the user asks to send (not draft) an email, you MUST plan to call both tools. Because sendDraft depends on the draftId from createDraft, generate createDraft first; after it is approved and the draftId is returned, call sendDraft in the continuation.
- Do not consider the email task complete after createDraft succeeds. After gmail.sendDraft succeeds, say the email was handed to Gmail for sending; do not claim the recipient received the email or that delivery succeeded.
For calendar.createEvent and calendar.updateEvent:
- Attendees must be valid email addresses.
- If the user provides a person name instead of an email address, call people.searchDirectory first and use the resolved Workspace email.
- Do not pass display names like "Bao" or "Tung" into attendees.
- If no matching email can be resolved, ask one concise clarification question for the attendee email.
For Google Chat tools:
- chat.sendMessage, chat.listMessages, chat.listMembers, and chat.addMember require space to be a Google Chat resource name like spaces/AAAA.
- If the user gives a group name, person name, or email instead of spaces/AAAA, do not put that raw name into space.
- Resolve the target first with people.searchDirectory plus chat.findSpacesByMembers when the user names people, or chat.listSpaces when the user names a space/group.
- For requests like "gửi tin nhắn vào nhóm chat VClaw" or "gửi file này vào nhóm VClaw", call chat.listSpaces first, match the requested group/display name from the returned spaces, then call chat.sendMessage with the matched spaces/... resource.
- Do not ask the user to provide spaces/AAAA until chat.listSpaces or member resolution has already failed or returned ambiguous matches.
- If the target space is still ambiguous after read-tool resolution, ask one concise clarification question before calling a write tool.
For channel attachments:
- If the user message contains "Attachment paths:", those are local files sent through the current channel.
- If the user says "file này", "file tôi đã gửi", "ảnh này", or asks to attach/send/upload the current file, use those paths in tool arguments that accept attachments.
- For Gmail drafts, use attachment paths in gmail.createDraft/gmail.updateDraft/gmail.replyDraft/gmail.forwardDraft attachments.
- For Google Chat messages, use attachment paths in chat.sendMessage attachments.
- Do not call gmail.downloadAttachments unless the user explicitly wants to download an attachment from an existing Gmail message.

Format final answers for chat channels:
- Start with one short summary line.
- For Gmail, Calendar, Chat, or People results, use compact bullets with the important fields only.
- Prefer 5 to 10 bullets unless the user asks for more.
- For Gmail list results, if the user asks to list every email, include every message in Messages and do not group by sender unless the user asks for unique senders.
- For Gmail list results, group relative-date answers by LocalDate. Date is the original email header and may use a different timezone.
- Do not dump raw JSON, raw tool outputs, internal tool names, or opaque IDs unless the user explicitly asks.
- Use plain text only. Do not use Markdown bold, italic, inline code, headings, or syntax markers like **, __, backticks, or #.
- Avoid Markdown tables because Telegram renders them poorly in plain text.
- If no relevant result is found, say that plainly and suggest the next useful query.`, now.Format(time.RFC3339)))
}

func (r *Runtime) resolveReference(ctx context.Context, message contracts.UserMessage, recentHistory []string, memory sessions.SessionMemory, activeClarification bool) (*reference.Resolution, *contracts.ErrorShape) {
	if r.referenceResolver == nil || activeClarification {
		return nil, nil
	}
	// Revision messages are structured internal requests built by buildRevisionRequest;
	// they contain tool names and keywords that would falsely trigger reference resolution.
	if isRevisionMessage(message) {
		return nil, nil
	}
	if !hasReferenceCueText(message.Text) {
		return nil, nil
	}
	resolution, err := r.referenceResolver.Resolve(ctx, reference.Input{
		CurrentMessage: message.Text,
		RecentHistory:  recentHistory,
		Memory:         memory,
		Now:            r.now(),
	})
	if err != nil {
		retryable := providers.IsRetryableError(err)
		code := contracts.ErrorProviderError
		if retryable {
			code = contracts.ErrorProviderUnavailable
		}
		return nil, &contracts.ErrorShape{
			Code:      code,
			Message:   "reference resolution failed: " + err.Error(),
			Source:    contracts.ErrorSourceProvider,
			Retryable: retryable,
		}
	}
	if resolution == nil {
		return nil, nil
	}
	r.logger.Info("reference resolved",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"has_reference", resolution.HasReference,
		"reference_type", resolution.ReferenceType,
		"source", resolution.Source,
		"confidence", resolution.Confidence,
		"needs_clarification", resolution.NeedsClarification,
	)
	return resolution, nil
}

func (r *Runtime) referenceClarificationResponse(message contracts.UserMessage, resolution *reference.Resolution) *contracts.AgentResponse {
	if resolution == nil || !resolution.HasReference || !resolution.NeedsClarification {
		return nil
	}
	if !hasReferenceCueText(message.Text) {
		return nil
	}
	question := strings.TrimSpace(resolution.ClarificationQuestion)
	if question == "" {
		question = "Bạn muốn nói tới mục nào trong cuộc trò chuyện trước đó?"
	}
	return &contracts.AgentResponse{
		RequestID: message.RequestID,
		SessionID: message.SessionID,
		Status:    contracts.AgentStatusNeedClarification,
		Message:   question,
		Data:      r.traceData(resolution),
	}
}

func referenceContextPrompt(resolution *reference.Resolution) string {
	if !isUsableReference(resolution) {
		return ""
	}
	context := "{}"
	if resolution.ResolvedContext != nil {
		if data, err := json.MarshalIndent(resolution.ResolvedContext, "", "  "); err == nil {
			context = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`Reference resolver result for the current user message:
- has_reference: %t
- reference_type: %s
- reference_id: %s
- source: %s
- confidence: %.2f
- resolved_context:
%s

Use this only to understand phrases like "lịch này", "cuộc họp trên", "email vừa rồi", or "chủ đề đó".
Do not expose this resolver output directly to the user.
Do not use reference memory as approval. For any write/destructive action, still call the matching tool and let runtime request approval before execution.`,
		resolution.HasReference,
		resolution.ReferenceType,
		strings.TrimSpace(resolution.ReferenceID),
		resolution.Source,
		resolution.Confidence,
		context,
	))
}

func (r *Runtime) providerTools() []providers.ToolDefinition {
	definitions := providers.ToolDefinitionsFromRegistry(r.registry.ListTools())
	definitions = append(definitions, clarifyToolDefinition())
	return definitions
}
func shouldRetryTextualApprovalAsToolCall(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	if !containsAnyText(lower,
		"tạo", "tao", "create",
		"gửi", "gui", "send",
		"xóa", "xoa", "delete",
		"cập nhật", "cap nhat", "update",
	) {
		return false
	}
	return containsAnyText(lower,
		"bạn xác nhận", "ban xac nhan",
		"bạn có xác nhận", "ban co xac nhan",
		"có xác nhận không", "co xac nhan khong",
		"vui lòng xác nhận", "vui long xac nhan",
		"xác nhận để", "xac nhan de",
		"tiến hành không", "tien hanh khong",
		"tiến hành nhé", "tien hanh nhe",
		"tiến hành tạo", "tien hanh tao",
		"tiến hành gửi", "tien hanh gui",
		"tiến hành xóa", "tien hanh xoa",
		"tiến hành cập nhật", "tien hanh cap nhat",
		"please confirm",
		"do you confirm",
		"confirm to proceed",
	)
}

func isSideEffectToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "calendar.createEvent",
		"calendar.updateEvent",
		"calendar.deleteEvent",
		"gmail.createDraft",
		"gmail.updateDraft",
		"gmail.sendDraft",
		"gmail.deleteDraft",
		"gmail.replyDraft",
		"gmail.forwardDraft",
		"gmail.downloadAttachments",
		"gmail.modifyMessage",
		"gmail.batchModifyMessages",
		"gmail.trashMessage",
		"gmail.untrashMessage",
		"chat.sendMessage",
		"chat.updateMessage",
		"chat.deleteMessage",
		"chat.createSpace",
		"chat.addMember",
		"chat.removeMember",
		"sandbox.runPython",
		"sandbox.runShell":
		return true
	default:
		return false
	}
}
