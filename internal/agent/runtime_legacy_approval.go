package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

func (r *Runtime) legacyApprovalRequest(message contracts.UserMessage, toolCall providers.ToolCall, decision contracts.RiskDecision) contracts.ApprovalRequest {
	now := r.now()
	// Stamp the governance bundle on the contract ToolCall before it leaves
	// Agent Core (docs/03-contracts.md §3.11). The same bundle is mirrored on
	// the ApprovalRequest so approval records are self-contained for audit.
	governanceMeta := r.buildGovernanceMetadata(toolCall.Name, decision.PolicyDecisionRef)
	contractCall := contracts.ToolCall{
		ToolCallID: toolCall.ID,
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolName:   toolCall.Name,
		Input:      cloneArguments(toolCall.Arguments),
		Governance: governanceMeta,
	}
	summary := approvalSummary(toolCall.Name, decision.RiskLevel)
	parentApprovalID := ""
	if message.Metadata != nil {
		if value, ok := message.Metadata["parentApprovalId"].(string); ok && strings.TrimSpace(value) != "" {
			parentApprovalID = strings.TrimSpace(value)
		} else if value, ok := message.Metadata["approvalId"].(string); ok && strings.TrimSpace(value) != "" {
			parentApprovalID = strings.TrimSpace(value)
		}
	}
	approval := contracts.ApprovalRequest{
		ApprovalID:       "appr_" + safeID(toolCall.ID),
		ParentApprovalID: parentApprovalID,
		RequestID:        message.RequestID,
		SessionID:        message.SessionID,
		ToolCallID:       toolCall.ID,
		Status:           contracts.ApprovalStatusPending,
		RiskLevel:        decision.RiskLevel,
		Summary:          summary,
		Details:          decision.Reason,
		ToolCall:         contractCall,
		CreatedAt:        now,
		ExpiresAt:        now.Add(approvalTTL),
		Governance:       governanceMeta,
	}
	r.logger.Info("approval request created",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"approval_id", approval.ApprovalID,
		"tool_call_id", toolCall.ID,
		"tool_name", toolCall.Name,
		"risk_level", approval.RiskLevel,
		"parent_approval_id", approval.ParentApprovalID,
	)
	return approval
}

func (r *Runtime) legacyStorePendingApproval(pending pendingApproval) {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	approvalID := strings.TrimSpace(pending.request.ApprovalID)
	sessionID := strings.TrimSpace(pending.message.SessionID)
	if approvalID == "" || sessionID == "" {
		return
	}
	if oldID := r.pendingBySession[sessionID]; oldID != "" {
		delete(r.pendingApprovals, oldID)
	}
	r.pendingApprovals[approvalID] = pending
	r.pendingBySession[sessionID] = approvalID
	r.logger.Info("approval request persisted",
		"request_id", pending.message.RequestID,
		"session_id", sessionID,
		"approval_id", approvalID,
		"tool_call_id", pending.request.ToolCallID,
		"tool_name", pending.request.ToolCall.ToolName,
	)
}

func (r *Runtime) legacyTakePendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		approvalID = r.pendingBySession[sessionID]
	}
	r.logger.Info("approval request lookup attempted",
		"session_id", sessionID,
		"approval_id", approvalID,
	)
	if approvalID == "" {
		return pendingApproval{}, false
	}
	pending, ok := r.pendingApprovals[approvalID]
	if !ok {
		r.logger.Info("approval request lookup failed or was already resolved",
			"session_id", sessionID,
			"approval_id", approvalID,
		)
		return pendingApproval{}, false
	}
	delete(r.pendingApprovals, approvalID)
	if sessionID == "" {
		sessionID = pending.message.SessionID
	}
	if r.pendingBySession[sessionID] == approvalID {
		delete(r.pendingBySession, sessionID)
	}
	return pending, true
}

func (r *Runtime) legacyPeekPendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		approvalID = r.pendingBySession[sessionID]
	}
	if approvalID == "" {
		return pendingApproval{}, false
	}
	pending, ok := r.pendingApprovals[approvalID]
	return pending, ok
}

func legacyBuildApprovalContinuationMessage(pending pendingApproval, result tools.ToolResult, now time.Time) contracts.UserMessage {
	var text string
	if len(pending.remainingToolCalls) > 0 {
		remainingNames := make([]string, 0, len(pending.remainingToolCalls))
		for _, tc := range pending.remainingToolCalls {
			remainingNames = append(remainingNames, tc.Name)
		}
		text = strings.TrimSpace(fmt.Sprintf(`Continuing the original multi-step request after an approved tool completed.
Luôn trả lời bằng tiếng Việt nếu người dùng đang nói tiếng Việt.
Do not repeat the tool that was just executed.

Original request:
%s

Completed tool: %s
Result: %s

Continue by calling the remaining tools in the original plan: %s
Use any resource IDs or names returned by the completed tool's result when they are needed as input for the next tool.`,
			pending.message.Text,
			pending.toolCall.Name,
			result.ContentForLLM,
			strings.Join(remainingNames, ", "),
		))
	} else {
		text = strings.TrimSpace(fmt.Sprintf(`An approved tool just completed as part of the user's original request.
Luôn trả lời bằng tiếng Việt nếu người dùng đang nói tiếng Việt.

Original request:
%s

Completed tool: %s
Result: %s

Check whether the original request contained additional tasks that have not yet been done.
If yes, call the necessary tool(s) now — do NOT ask the user again for information already given in the original request.
If all tasks are already complete, respond with a short Vietnamese summary of what was accomplished.
Do not repeat the tool that was just executed.`,
			pending.message.Text,
			pending.toolCall.Name,
			result.ContentForLLM,
		))
	}
	msg := pending.message
	msg.Text = text
	msg.Timestamp = now
	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}
	msg.Metadata["continuationOf"] = pending.request.ApprovalID
	msg.Metadata["completedTool"] = pending.toolCall.Name
	return msg
}

func legacyBuildRevisionRequest(pending pendingApproval, comment string) string {
	input := "{}"
	if len(pending.toolCall.Arguments) > 0 {
		if data, err := json.MarshalIndent(pending.toolCall.Arguments, "", "  "); err == nil {
			input = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`Người dùng muốn chỉnh lại một yêu cầu đang chờ xác nhận.
Luôn trả lời bằng tiếng Việt nếu người dùng đang nói tiếng Việt.
Không thực thi tool call cũ đang chờ.
Dùng yêu cầu ban đầu, input tool đang chờ, và ghi chú chỉnh sửa để tạo plan/tool call mới.
Nếu hành động sau khi chỉnh vẫn có side effect, hãy gọi tool tương ứng để runtime tạo yêu cầu xác nhận mới.
Nếu còn thiếu thông tin, hỏi một câu ngắn gọn bằng tiếng Việt.

Yêu cầu ban đầu:
%s

Tool đang chờ:
%s

Input đang chờ:
%s

Ghi chú chỉnh sửa:
%s`, pending.message.Text, pending.request.ToolCall.ToolName, input, comment))
}

func legacyApprovalSummary(toolName string, riskLevel contracts.RiskLevel) string {
	switch toolName {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		return "Tôi cần bạn xác nhận trước khi tạo hoặc sửa Gmail draft."
	case "gmail.sendDraft":
		return "Tôi cần bạn xác nhận trước khi gửi email."
	case "gmail.deleteDraft":
		return "Tôi cần bạn xác nhận trước khi xóa Gmail draft."
	case "gmail.downloadAttachments":
		return "Tôi cần bạn xác nhận trước khi tải attachment Gmail xuống máy local."
	case "gmail.modifyMessage", "gmail.batchModifyMessages":
		return "Tôi cần bạn xác nhận trước khi sửa trạng thái hoặc nhãn Gmail."
	case "gmail.trashMessage":
		return "Tôi cần bạn xác nhận trước khi chuyển email vào thùng rác."
	case "gmail.untrashMessage":
		return "Tôi cần bạn xác nhận trước khi khôi phục email khỏi thùng rác."
	case "calendar.createEvent":
		return "Tôi cần bạn xác nhận trước khi tạo sự kiện Calendar."
	case "calendar.updateEvent":
		return "Tôi cần bạn xác nhận trước khi sửa sự kiện Calendar."
	case "calendar.deleteEvent":
		return "Tôi cần bạn xác nhận trước khi xóa sự kiện Calendar."
	case "chat.sendMessage":
		return "Tôi cần bạn xác nhận trước khi gửi tin nhắn Google Chat."
	case "chat.updateMessage":
		return "Tôi cần bạn xác nhận trước khi sửa tin nhắn Google Chat."
	case "chat.deleteMessage":
		return "Tôi cần bạn xác nhận trước khi xóa tin nhắn Google Chat."
	case "chat.createSpace":
		return "Tôi cần bạn xác nhận trước khi tạo Google Chat space."
	case "chat.addMember":
		return "Tôi cần bạn xác nhận trước khi thêm thành viên Google Chat."
	case "chat.removeMember":
		return "Tôi cần bạn xác nhận trước khi xóa thành viên Google Chat."
	case "sandbox.runPython", "sandbox.runShell":
		return "Tôi cần bạn xác nhận trước khi chạy code hoặc lệnh trong sandbox."
	default:
		return fmt.Sprintf("Tôi cần bạn xác nhận trước khi chạy %s vì risk là %s.", toolName, riskLevel)
	}
}

func legacyApprovalExecutionMessage(result tools.ToolResult, contractResult contracts.ToolResult) string {
	if rendered := renderToolResultForUser(contractResult); rendered != "" {
		return rendered
	}
	if strings.TrimSpace(result.ContentForUser) != "" {
		return formatOutboundText(result.ContentForUser)
	}
	if result.Success {
		return "Đã thực hiện thao tác sau khi bạn xác nhận."
	}
	if result.Error != nil && strings.TrimSpace(result.Error.Message) != "" {
		return result.Error.Message
	}
	return "Tool không hoàn tất."
}

func providerToolCallToToolCall(call providers.ToolCall) tools.ToolCall {
	return tools.ToolCall{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: cloneArguments(call.Arguments),
	}
}

// contractToolResult converts a tools.ToolResult into the canonical
// contracts.ToolResult that crosses Agent-Core boundaries (channel, audit,
// store). The governance argument carries the full provenance bundle the
// runtime stamped on the originating ToolCall (model, promptVersion,
// toolSchemaVersion, policyDecisionRef) — pass nil from contexts where the
// runtime cannot compute it (e.g. unit tests). The result's own
// PolicyDecisionRef still wins when governance is nil so the tool layer's
// policy stamp survives. See docs/03-contracts.md §3.11.
func contractToolResult(result tools.ToolResult, governance *contracts.GovernanceMetadata) contracts.ToolResult {
	contractResult := contracts.ToolResult{
		ToolCallID: result.ToolCallID,
		ToolName:   result.ToolName,
		Success:    result.Success,
		Data: map[string]any{
			"contentForUser": result.ContentForUser,
			"contentForLLM":  result.ContentForLLM,
		},
		ArtifactRef: convertToolArtifactRef(result.ArtifactRef),
		Metadata:    cloneMetadataMap(result.Metadata),
		Truncated:   result.Truncated,
		Redacted:    result.Redacted,
		Source:      result.Source,
	}
	if result.Error != nil {
		contractResult.Error = toolErrorShape(result)
	}
	contractResult.Governance = mergeToolResultGovernance(governance, result.PolicyDecisionRef)
	return contractResult
}

// mergeToolResultGovernance combines the runtime-supplied governance bundle
// with the PolicyDecisionRef the tool layer stamped on the result. The result
// stamp wins when both are present (the tool layer is the authoritative source
// for the live policy reference at execution time). Returns nil when every
// field is empty so JSON output stays compact for un-instrumented paths.
func mergeToolResultGovernance(base *contracts.GovernanceMetadata, resultPolicyRef string) *contracts.GovernanceMetadata {
	resultPolicyRef = strings.TrimSpace(resultPolicyRef)
	if base == nil {
		if resultPolicyRef == "" {
			return nil
		}
		return &contracts.GovernanceMetadata{PolicyDecisionRef: resultPolicyRef}
	}
	merged := *base
	if resultPolicyRef != "" {
		merged.PolicyDecisionRef = resultPolicyRef
	}
	if merged.Model == "" && merged.PromptVersion == "" && merged.ToolSchemaVersion == "" && merged.PolicyDecisionRef == "" {
		return nil
	}
	return &merged
}

func toolErrorShape(result tools.ToolResult) *contracts.ErrorShape {
	if result.Error == nil {
		return internalError("tool failed without error shape", contracts.ErrorSourceTool)
	}
	code := result.Error.Code
	switch code {
	case tools.ErrorToolNotFound:
		code = contracts.ErrorToolNotFound
	case tools.ErrorInvalidArgument:
		code = contracts.ErrorToolInputInvalid
	case tools.ErrorBlockedByPolicy:
		code = contracts.ErrorActionBlockedByPolicy
	case tools.ErrorTimeout:
		code = contracts.ErrorProviderTimeout
	case tools.ErrorExecutionFailed:
		code = contracts.ErrorInternal
	}
	return &contracts.ErrorShape{
		Code:      code,
		Message:   result.Error.Message,
		Source:    contracts.ErrorSourceTool,
		Retryable: false,
	}
}

func internalError(message string, source contracts.ErrorSource) *contracts.ErrorShape {
	return &contracts.ErrorShape{
		Code:      contracts.ErrorInternal,
		Message:   message,
		Source:    source,
		Retryable: false,
	}
}

func policyErrorCode(found bool) string {
	if !found {
		return contracts.ErrorToolNotFound
	}
	return contracts.ErrorActionBlockedByPolicy
}

func safeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "toolcall"
	}
	return strings.NewReplacer(" ", "_", "/", "_", "\\", "_").Replace(id)
}

func (r *Runtime) legacyHasPendingApproval(_ context.Context, sessionID string) bool {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	approvalID := r.pendingBySession[strings.TrimSpace(sessionID)]
	if approvalID == "" {
		return false
	}
	_, ok := r.pendingApprovals[approvalID]
	return ok
}

func (r *Runtime) legacyResolveApproval(ctx context.Context, sessionID string, decision contracts.ApprovalDecision) (contracts.AgentResponse, error) {
	switch decision.Decision {
	case contracts.ApprovalDecisionRevised:
		return r.ReviseApproval(ctx, sessionID, decision.RequestID, decision.ApprovalID, decision.Comment)
	}

	pending, ok := r.takePendingApproval(sessionID, decision.ApprovalID)
	if !ok {
		r.logger.Info("approval request lookup failed or was already resolved",
			"request_id", decision.RequestID,
			"session_id", sessionID,
			"approval_id", strings.TrimSpace(decision.ApprovalID),
		)
		return contracts.AgentResponse{
			RequestID: decision.RequestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không tìm thấy yêu cầu xác nhận đang chờ.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalNotFound,
				Message:   "pending approval not found",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	if pending.request.ExpiresAt.Before(r.now()) {
		r.logger.Info("approval decision received for expired request",
			"request_id", decision.RequestID,
			"session_id", sessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusExpired,
		)
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Yêu cầu xác nhận đã hết hạn. Vui lòng gửi lại yêu cầu.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalExpired,
				Message:   "approval expired",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	switch decision.Decision {
	case contracts.ApprovalDecisionApproved:
		r.logger.Info("approval request status updated",
			"request_id", pending.message.RequestID,
			"session_id", pending.message.SessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusApproved,
		)
		r.logger.Info("runtime resuming pending tool after approval",
			"request_id", pending.message.RequestID,
			"session_id", pending.message.SessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
		)
		execCtx := toolhooks.WithRequestContext(ctx, pending.message.RequestID, pending.message.SessionID)
		decision := r.approvedToolDecision(execCtx, pending.toolCall, pending.definition, true)
		result := toolDecisionDeniedResult(pending.toolCall, decision)
		if decision.Decision != contracts.RiskDecisionBlock {
			result = r.executeAllowedTool(execCtx, pending.toolCall, pending.definition)
		}
		if errShape := r.recordActionResult(ctx, pending.message.SessionID, result); errShape != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
		contractResult := contractToolResult(result, r.buildGovernanceMetadata(pending.toolCall.Name, result.PolicyDecisionRef))
		response := contracts.AgentResponse{
			RequestID:   pending.message.RequestID,
			SessionID:   pending.message.SessionID,
			Status:      contracts.AgentStatusCompleted,
			Data:        r.traceData(nil, nil, nil),
			ToolResults: []contracts.ToolResult{contractResult},
		}
		response.Message = approvalExecutionMessage(result, contractResult)
		if !result.Success {
			response.Status = contracts.AgentStatusFailed
			response.Error = toolErrorShape(result)
			response.Message = response.Error.Message
		}
		if errShape := r.appendAssistantTranscript(ctx, pending.message.SessionID, response.Message); errShape != nil {
			response.Status = contracts.AgentStatusFailed
			response.Error = errShape
			response.Message = errShape.Message
		}
		// After a successful approval, always run a continuation pass so that
		// remaining tasks from the original multi-step request are not lost.
		// If remainingToolCalls is non-empty (same-batch siblings), the continuation
		// replays them explicitly. If empty, the continuation gives the LLM a chance
		// to detect and execute any tasks from the original request not yet done.
		if result.Success {
			continuation := buildApprovalContinuationMessage(pending, result, r.now())
			r.logger.Info("runtime received resume signal for continuation after approval",
				"request_id", pending.message.RequestID,
				"session_id", pending.message.SessionID,
				"approval_id", pending.request.ApprovalID,
				"tool_call_id", pending.request.ToolCallID,
			)
			if continuationResp, err := r.Run(ctx, continuation); err == nil {
				return continuationResp, nil
			}
		}
		return response, nil
	case contracts.ApprovalDecisionRejected:
		r.logger.Info("approval request status updated",
			"request_id", pending.message.RequestID,
			"session_id", pending.message.SessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusRejected,
		)
		comment := strings.TrimSpace(decision.Comment)
		if comment != "" {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusNeedClarification,
				Message:   "Đã hủy thao tác đang chờ. Bạn muốn chỉnh lại như thế nào?\n\nGhi chú của bạn: " + comment,
				Data:      r.traceData(nil, nil, nil),
			}, nil
		}
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusBlocked,
			Message:   "Đã hủy thao tác. Tôi chưa thực hiện tool nào.",
			Data:      r.traceData(nil, nil, nil),
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorActionBlockedByPolicy,
				Message:   "approval rejected",
				Source:    contracts.ErrorSourcePolicy,
				Retryable: false,
			},
		}, nil
	default:
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Quyết định xác nhận không hợp lệ.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorInvalidInput,
				Message:   "approval decision must be approved, rejected, or revised",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}
}

func (r *Runtime) legacyReviseApproval(ctx context.Context, sessionID string, requestID string, approvalID string, comment string) (contracts.AgentResponse, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "Tôi muốn chỉnh lại yêu cầu đang chờ xác nhận."
	}
	pending, ok := r.takePendingApproval(sessionID, approvalID)
	if !ok {
		r.logger.Info("approval request lookup failed",
			"request_id", requestID,
			"session_id", sessionID,
			"approval_id", strings.TrimSpace(approvalID),
		)
		return contracts.AgentResponse{
			RequestID: requestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không tìm thấy yêu cầu xác nhận đang chờ.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalNotFound,
				Message:   "pending approval not found",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}
	if pending.request.ExpiresAt.Before(r.now()) {
		r.logger.Info("approval decision received for expired request",
			"request_id", requestID,
			"session_id", sessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusExpired,
		)
		return contracts.AgentResponse{
			RequestID: requestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Yêu cầu xác nhận đã hết hạn. Vui lòng gửi lại yêu cầu.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalExpired,
				Message:   "approval expired",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	revisionText := buildRevisionRequest(pending, comment)
	revisionMessage := pending.message
	revisionMessage.RequestID = strings.TrimSpace(requestID)
	if revisionMessage.RequestID == "" {
		revisionMessage.RequestID = pending.message.RequestID
	}
	revisionMessage.Text = revisionText
	revisionMessage.Timestamp = r.now()
	if revisionMessage.Metadata == nil {
		revisionMessage.Metadata = map[string]any{}
	}
	revisionMessage.Metadata["approvalId"] = pending.request.ApprovalID
	revisionMessage.Metadata["parentApprovalId"] = pending.request.ApprovalID
	revisionMessage.Metadata["revisionComment"] = comment
	r.logger.Info("approval request status updated",
		"request_id", pending.message.RequestID,
		"session_id", pending.message.SessionID,
		"approval_id", pending.request.ApprovalID,
		"tool_call_id", pending.request.ToolCallID,
		"old_status", pending.request.Status,
		"new_status", contracts.ApprovalStatusRevised,
	)

	return r.Run(ctx, revisionMessage)
}
