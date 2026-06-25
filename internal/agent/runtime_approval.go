package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/orchestration"
	"vclaw/internal/providers"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

func (r *Runtime) HasPendingApproval(ctx context.Context, sessionID string) bool {
	r.approvalMu.Lock()
	approvalID := r.pendingBySession[strings.TrimSpace(sessionID)]
	_, ok := r.pendingApprovals[approvalID]
	r.approvalMu.Unlock()
	if ok {
		return true
	}
	if r.stateStore == nil {
		return false
	}
	_, err := r.stateStore.FindLatestPendingApproval(ctx, strings.TrimSpace(sessionID))
	return err == nil
}

func (r *Runtime) ResolveApproval(ctx context.Context, sessionID string, decision contracts.ApprovalDecision) (contracts.AgentResponse, error) {
	if decision.Decision == contracts.ApprovalDecisionRevised {
		return r.ReviseApproval(ctx, sessionID, decision.RequestID, decision.ApprovalID, decision.Comment)
	}

	pending, ok := r.takePendingApproval(sessionID, decision.ApprovalID)
	if !ok {
		var errShape *contracts.ErrorShape
		pending, ok, errShape = r.pendingApprovalFromState(ctx, sessionID, decision.ApprovalID)
		if errShape != nil {
			return contracts.AgentResponse{
				RequestID: decision.RequestID,
				SessionID: sessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
		if !ok {
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
	}

	if pending.request.ExpiresAt.Before(r.now()) {
		if pending.actionID != "" {
			if _, err := r.stateStore.MarkActionExpired(ctx, pending.actionID); err != nil && !errors.Is(err, ErrRuntimeStateNotFound) {
				return contracts.AgentResponse{
					RequestID: pending.message.RequestID,
					SessionID: pending.message.SessionID,
					Status:    contracts.AgentStatusFailed,
					Message:   "Không thể cập nhật approval đã hết hạn.",
					Error:     internalError("expire action: "+err.Error(), contracts.ErrorSourceAgent),
				}, nil
			}
			r.recordApprovalObservation(ActionStatusExpired)
			if r.telemetry != nil {
				r.telemetry.RecordApproval(ctx, ApprovalTelemetryEvent{
					Status:     ActionStatusExpired,
					ApprovalID: pending.request.ApprovalID,
					RequestID:  pending.message.RequestID,
					SessionID:  pending.message.SessionID,
					ToolCallID: pending.request.ToolCallID,
					ToolName:   pending.toolCall.Name,
					RiskLevel:  pending.request.RiskLevel,
				})
			}
		}
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed, string(orchestration.FailureReasonApprovalExpired)); errShape != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
		return contracts.AgentResponse{
			RequestID:     pending.message.RequestID,
			SessionID:     pending.message.SessionID,
			Status:        contracts.AgentStatusFailed,
			Message:       "Yêu cầu xác nhận đã hết hạn. Vui lòng gửi lại yêu cầu.",
			FailureReason: string(orchestration.FailureReasonApprovalExpired),
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
		if pending.actionID == "" {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   "Không tìm thấy action tương ứng với approval.",
				Error:     internalError("approval action id is missing", contracts.ErrorSourceAgent),
			}, nil
		}
		if _, err := r.stateStore.MarkActionApproved(ctx, pending.actionID, approvalDecisionRecord(sessionID, decision)); err != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   "Không thể cập nhật approval.",
				Error:     internalError("approve action: "+err.Error(), contracts.ErrorSourceAgent),
			}, nil
		}
		r.appendRunEvent(ctx, pending.runID, "approval.approved", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.request.ToolCall.ToolName,
		})
		r.recordApprovalObservation(ActionStatusApproved)
		if r.telemetry != nil {
			r.telemetry.RecordApproval(ctx, ApprovalTelemetryEvent{
				Status:     ActionStatusApproved,
				ApprovalID: pending.request.ApprovalID,
				RequestID:  pending.message.RequestID,
				SessionID:  pending.message.SessionID,
				ToolCallID: pending.request.ToolCallID,
				ToolName:   pending.toolCall.Name,
				RiskLevel:  pending.request.RiskLevel,
				Comment:    decision.Comment,
			})
		}
		return r.resumeApprovedAction(ctx, pending)
	case contracts.ApprovalDecisionRejected:
		if pending.actionID != "" {
			if _, err := r.stateStore.MarkActionRejected(ctx, pending.actionID, approvalDecisionRecord(sessionID, decision)); err != nil && !errors.Is(err, ErrRuntimeStateNotFound) {
				return contracts.AgentResponse{
					RequestID: pending.message.RequestID,
					SessionID: pending.message.SessionID,
					Status:    contracts.AgentStatusFailed,
					Message:   "Không thể cập nhật approval bị từ chối.",
					Error:     internalError("reject action: "+err.Error(), contracts.ErrorSourceAgent),
				}, nil
			}
			r.recordApprovalObservation(ActionStatusRejected)
			if r.telemetry != nil {
				r.telemetry.RecordApproval(ctx, ApprovalTelemetryEvent{
					Status:     ActionStatusRejected,
					ApprovalID: pending.request.ApprovalID,
					RequestID:  pending.message.RequestID,
					SessionID:  pending.message.SessionID,
					ToolCallID: pending.request.ToolCallID,
					ToolName:   pending.toolCall.Name,
					RiskLevel:  pending.request.RiskLevel,
					Comment:    decision.Comment,
				})
			}
		}
		r.appendRunEvent(ctx, pending.runID, "approval.rejected", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.request.ToolCall.ToolName,
			"comment":    strings.TrimSpace(decision.Comment),
		})
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusBlocked, string(orchestration.FailureReasonApprovalRejected)); errShape != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
		comment := strings.TrimSpace(decision.Comment)
		if comment != "" {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusNeedClarification,
				Message:   "Đã hủy thao tác đang chờ. Bạn muốn chỉnh lại như thế nào?\n\nGhi chú của bạn: " + comment,
				Data:      r.traceData(nil),
			}, nil
		}
		return contracts.AgentResponse{
			RequestID:     pending.message.RequestID,
			SessionID:     pending.message.SessionID,
			Status:        contracts.AgentStatusBlocked,
			Message:       "Đã hủy thao tác. Tôi chưa thực hiện tool nào.",
			FailureReason: string(orchestration.FailureReasonApprovalRejected),
			Data:          r.traceData(nil),
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
				Message:   "approval decision must be approved or rejected",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}
}

func (r *Runtime) ReviseApproval(ctx context.Context, sessionID string, requestID string, approvalID string, comment string) (contracts.AgentResponse, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "Tôi muốn chỉnh lại yêu cầu đang chờ xác nhận."
	}
	pending, ok := r.peekPendingApproval(sessionID, approvalID)
	if !ok {
		var errShape *contracts.ErrorShape
		pending, ok, errShape = r.pendingApprovalFromState(ctx, sessionID, approvalID)
		if errShape != nil {
			return contracts.AgentResponse{
				RequestID: requestID,
				SessionID: sessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
		if !ok {
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
	}
	if pending.request.ExpiresAt.Before(r.now()) {
		r.takePendingApproval(sessionID, approvalID)
		if pending.actionID != "" {
			if _, err := r.stateStore.MarkActionExpired(ctx, pending.actionID); err != nil && !errors.Is(err, ErrRuntimeStateNotFound) {
				return contracts.AgentResponse{
					RequestID: requestID,
					SessionID: sessionID,
					Status:    contracts.AgentStatusFailed,
					Message:   "Không thể cập nhật approval đã hết hạn.",
					Error:     internalError("expire action: "+err.Error(), contracts.ErrorSourceAgent),
				}, nil
			}
			r.recordApprovalObservation(ActionStatusExpired)
			if r.telemetry != nil {
				r.telemetry.RecordApproval(ctx, ApprovalTelemetryEvent{
					Status:     ActionStatusExpired,
					ApprovalID: pending.request.ApprovalID,
					RequestID:  requestID,
					SessionID:  sessionID,
					ToolCallID: pending.request.ToolCallID,
					ToolName:   pending.toolCall.Name,
					RiskLevel:  pending.request.RiskLevel,
				})
			}
		}
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed, string(orchestration.FailureReasonApprovalExpired)); errShape != nil {
			return contracts.AgentResponse{
				RequestID: requestID,
				SessionID: sessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
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

	return r.Run(ctx, revisionMessage)
}

func (r *Runtime) approvalRequest(message contracts.UserMessage, toolCall providers.ToolCall, decision contracts.RiskDecision, transcript []providers.Message) contracts.ApprovalRequest {
	now := r.now()
	input := cloneArguments(toolCall.Arguments)
	input = enrichApprovalInput(toolCall.Name, input, transcript)
	// Stamp the full governance bundle on the contract ToolCall before it
	// leaves Agent Core (see docs/03-contracts.md §3.11). The same bundle is
	// also attached to the ApprovalRequest so approval records are
	// self-contained for audit/N4 without a join back to tool_calls.
	governanceMeta := r.buildGovernanceMetadata(toolCall.Name, decision.PolicyDecisionRef)
	contractCall := contracts.ToolCall{
		ToolCallID: toolCall.ID,
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolName:   toolCall.Name,
		Input:      input,
		Governance: governanceMeta,
	}
	summary := approvalSummary(toolCall.Name, decision.RiskLevel, input)
	if toolCall.Name == "chat.sendMessage" {
		if email, ok := input["recipientEmail"].(string); ok && strings.TrimSpace(email) != "" {
			summary = "Mình sẽ gửi tin nhắn Google Chat. Xác nhận không?"
		}
	}
	parentApprovalID := ""
	if message.Metadata != nil {
		if value, ok := message.Metadata["parentApprovalId"].(string); ok && strings.TrimSpace(value) != "" {
			parentApprovalID = strings.TrimSpace(value)
		} else if value, ok := message.Metadata["approvalId"].(string); ok && strings.TrimSpace(value) != "" {
			parentApprovalID = strings.TrimSpace(value)
		}
	}
	return contracts.ApprovalRequest{
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
}

func approvalDecisionRecord(sessionID string, decision contracts.ApprovalDecision) ApprovalDecisionRecord {
	return ApprovalDecisionRecord{
		RequestID: decision.RequestID,
		SessionID: sessionID,
		Decision:  decision.Decision,
		DecidedBy: decision.DecidedBy,
		Channel:   decision.Channel,
		Comment:   decision.Comment,
		DecidedAt: decision.DecidedAt,
	}
}

func enrichApprovalInput(toolName string, input map[string]any, transcript []providers.Message) map[string]any {
	switch strings.TrimSpace(toolName) {
	case "drive.moveFile", "drive.moveFiles":
		return enrichDriveMoveApprovalInput(input, transcript)
	case "calendar.respondEvent":
		return enrichCalendarRespondApprovalInput(input, transcript)
	default:
		return input
	}
}

func enrichCalendarRespondApprovalInput(input map[string]any, transcript []providers.Message) map[string]any {
	if len(input) == 0 {
		return input
	}
	if strings.TrimSpace(stringArgument(input, "eventTitle")) != "" || strings.TrimSpace(stringArgument(input, "title")) != "" {
		return input
	}
	eventID := strings.TrimSpace(stringArgument(input, "eventId"))
	if eventID == "" {
		return input
	}
	title := calendarEventTitleByIDFromTranscript(transcript, eventID)
	if title == "" {
		return input
	}
	enriched := cloneArguments(input)
	enriched["eventTitle"] = title
	return enriched
}

func calendarEventTitleByIDFromTranscript(transcript []providers.Message, eventID string) string {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ""
	}
	for i := len(transcript) - 1; i >= 0; i-- {
		message := transcript[i]
		if message.Role != providers.MessageRoleTool || strings.TrimSpace(message.Content) == "" {
			continue
		}
		if title := calendarEventTitleFromJSON(message.Content, eventID); title != "" {
			return title
		}
	}
	return ""
}

func calendarEventTitleFromJSON(content string, eventID string) string {
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return ""
	}
	return calendarEventTitleFromPayload(payload, eventID)
}

func calendarEventTitleFromPayload(payload any, eventID string) string {
	switch typed := payload.(type) {
	case []any:
		for _, item := range typed {
			if title := calendarEventTitleFromPayload(item, eventID); title != "" {
				return title
			}
		}
	case map[string]any:
		if event, ok := typed["Event"]; ok {
			if title := calendarEventTitleFromPayload(event, eventID); title != "" {
				return title
			}
		}
		id := firstStringMapValue(typed, "id", "ID", "eventId", "EventID")
		if strings.TrimSpace(id) == eventID {
			return firstStringMapValue(typed, "title", "Title", "summary", "Summary", "eventTitle")
		}
	}
	return ""
}

func enrichDriveMoveApprovalInput(input map[string]any, transcript []providers.Message) map[string]any {
	if len(input) == 0 {
		return input
	}
	driveFiles := driveFilesByIDFromTranscript(transcript)
	if len(driveFiles) == 0 {
		return input
	}

	enriched := cloneArguments(input)
	fileIDs := stringSliceArg(enriched, "fileIds")
	if len(fileIDs) == 0 {
		if fileID := strings.TrimSpace(stringArgument(enriched, "fileId")); fileID != "" {
			fileIDs = []string{fileID}
		}
	}
	if len(fileIDs) > 0 {
		sources := make([]string, 0, len(fileIDs))
		for _, fileID := range fileIDs {
			sources = append(sources, driveApprovalDisplayName(fileID, driveFiles[fileID]))
		}
		enriched["sourceFiles"] = sources
	}
	if targetParentID := strings.TrimSpace(stringArgument(enriched, "targetParentId")); targetParentID != "" {
		enriched["targetFolder"] = driveApprovalDisplayName(targetParentID, driveFiles[targetParentID])
	}
	return enriched
}

type driveApprovalFileRef struct {
	ID       string
	Name     string
	MimeType string
}

func driveFilesByIDFromTranscript(transcript []providers.Message) map[string]driveApprovalFileRef {
	files := map[string]driveApprovalFileRef{}
	for _, message := range transcript {
		if message.Role != providers.MessageRoleTool || strings.TrimSpace(message.Content) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(message.Content), &payload); err != nil {
			continue
		}
		items, ok := payload["Files"].([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			fileMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ref := driveApprovalFileRef{
				ID:       firstStringMapValue(fileMap, "id", "ID"),
				Name:     firstStringMapValue(fileMap, "name", "Name"),
				MimeType: firstStringMapValue(fileMap, "mimeType", "MimeType"),
			}
			if strings.TrimSpace(ref.ID) != "" {
				files[ref.ID] = ref
			}
		}
	}
	return files
}

func driveApprovalDisplayName(id string, ref driveApprovalFileRef) string {
	id = strings.TrimSpace(id)
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return id
	}
	if id == "" {
		return name
	}
	return fmt.Sprintf("%s (ID: %s)", name, id)
}

func firstStringMapValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *Runtime) storePendingApproval(pending pendingApproval) {
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
}

func (r *Runtime) takePendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
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
	if !ok {
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

func (r *Runtime) peekPendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
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

func (r *Runtime) pendingApprovalFromState(ctx context.Context, sessionID string, approvalID string) (pendingApproval, bool, *contracts.ErrorShape) {
	if r.stateStore == nil {
		return pendingApproval{}, false, nil
	}
	approvalID = strings.TrimSpace(approvalID)
	sessionID = strings.TrimSpace(sessionID)
	var record ActionRecord
	var err error
	if approvalID == "" {
		record, err = r.stateStore.FindLatestPendingApproval(ctx, sessionID)
	} else {
		record, err = r.stateStore.GetActionByApprovalID(ctx, approvalID)
	}
	if err != nil {
		if errors.Is(err, ErrRuntimeStateNotFound) {
			return pendingApproval{}, false, nil
		}
		return pendingApproval{}, false, internalError("load approval action: "+err.Error(), contracts.ErrorSourceAgent)
	}
	if sessionID != "" && strings.TrimSpace(record.SessionID) != sessionID {
		return pendingApproval{}, false, nil
	}
	if !isResolvableApprovalActionStatus(record.Status) {
		return pendingApproval{}, false, nil
	}
	if record.Status == ActionStatusPendingApproval && approvalID != "" && !r.isLatestPendingApproval(ctx, record) {
		return pendingApproval{}, false, nil
	}
	definition, found := r.registry.GetDefinition(record.ToolName)
	if !found {
		definition.Name = record.ToolName
	}
	message := contracts.UserMessage{
		RequestID: record.RequestID,
		SessionID: record.SessionID,
		Channel:   "runtime",
		Text:      "Continue the original request after approval.",
		Timestamp: r.now(),
	}
	if runState, err := r.stateStore.GetRun(ctx, record.RunID); err == nil && strings.TrimSpace(runState.OriginalGoal) != "" {
		message.Text = runState.OriginalGoal
	}
	// Rebuild the governance bundle from the persisted action record so the
	// restored ApprovalRequest carries the same provenance the run had at
	// approval time, even if the live runtime has been reconstructed with a
	// different model/prompt/registry since then.
	governanceMeta := GovernanceFromActionRecord(record)
	request := contracts.ApprovalRequest{
		ApprovalID: record.ApprovalID,
		RequestID:  record.RequestID,
		SessionID:  record.SessionID,
		ToolCallID: record.ToolCallID,
		Status:     approvalStatusForAction(record.Status),
		RiskLevel:  record.RiskLevel,
		Summary:    approvalSummary(record.ToolName, record.RiskLevel, record.ArgsSnapshot),
		ToolCall: contracts.ToolCall{
			ToolCallID: record.ToolCallID,
			RequestID:  record.RequestID,
			SessionID:  record.SessionID,
			ToolName:   record.ToolName,
			Input:      cloneArguments(record.ArgsSnapshot),
			Governance: governanceMeta,
		},
		CreatedAt:  record.CreatedAt,
		ExpiresAt:  record.ApprovalExpiresAt,
		Governance: governanceMeta,
	}
	return pendingApproval{
		runID:      record.RunID,
		actionID:   record.ActionID,
		message:    message,
		request:    request,
		toolCall:   providers.ToolCall{ID: record.ToolCallID, Name: record.ToolName, Arguments: cloneArguments(record.ArgsSnapshot)},
		definition: definition,
	}, true, nil
}

func (r *Runtime) isLatestPendingApproval(ctx context.Context, record ActionRecord) bool {
	latest, err := r.stateStore.FindLatestPendingApproval(ctx, record.SessionID)
	if err != nil {
		return errors.Is(err, ErrRuntimeStateNotFound)
	}
	return latest.ActionID == record.ActionID
}

func isResolvableApprovalActionStatus(status ActionStatus) bool {
	return status == ActionStatusPendingApproval
}

func approvalStatusForAction(status ActionStatus) contracts.ApprovalStatus {
	switch status {
	case ActionStatusApproved, ActionStatusExecuting, ActionStatusCompleted:
		return contracts.ApprovalStatusApproved
	case ActionStatusRejected:
		return contracts.ApprovalStatusRejected
	case ActionStatusExpired:
		return contracts.ApprovalStatusExpired
	case ActionStatusSuperseded:
		return contracts.ApprovalStatusCancelled
	default:
		return contracts.ApprovalStatusPending
	}
}

func (r *Runtime) resumeApprovedAction(ctx context.Context, pending pendingApproval) (contracts.AgentResponse, error) {
	record, claimed, err := r.stateStore.TryMarkActionExecuting(ctx, pending.actionID)
	if err != nil {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không thể tiếp tục thao tác đã xác nhận.",
			Error:     internalError("mark action executing: "+err.Error(), contracts.ErrorSourceAgent),
		}, nil
	}
	if !claimed {
		return r.responseForUnclaimedApprovedAction(record, pending), nil
	}
	if errShape := r.resumeRunState(ctx, pending.runID); errShape != nil {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   errShape.Message,
			Error:     errShape,
		}, nil
	}

	startedAt := time.Now()
	execCtx := toolhooks.WithRequestContext(ctx, pending.message.RequestID, pending.message.SessionID)
	decision := r.approvedToolDecision(execCtx, pending.toolCall, pending.definition, true)
	result := toolDecisionDeniedResult(pending.toolCall, decision)
	if decision.Decision != contracts.RiskDecisionBlock {
		result = r.executeAllowedTool(execCtx, pending.toolCall, pending.definition)
	}
	// Carry the policy reference recorded on the action so the persisted
	// tool_calls row matches the risk_decisions row and N4 can join on it.
	result.PolicyDecisionRef = record.PolicyDecisionRef
	if errShape := r.recordRuntimeToolCall(ctx, nil, record.RunID, pending.toolCall, result, time.Since(startedAt), record.ApprovalID); errShape != nil {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   errShape.Message,
			Error:     errShape,
		}, nil
	}
	if result.Success {
		if _, err := r.stateStore.CompleteAction(ctx, pending.actionID, result); err != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   "Không thể lưu kết quả action.",
				Error:     internalError("complete action: "+err.Error(), contracts.ErrorSourceAgent),
			}, nil
		}
		r.appendRunEvent(ctx, record.RunID, "approval.resolved", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.toolCall.Name,
			"success":    true,
		})
	} else if _, err := r.stateStore.FailAction(ctx, pending.actionID, result); err != nil {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không thể lưu lỗi action.",
			Error:     internalError("fail action: "+err.Error(), contracts.ErrorSourceAgent),
		}, nil
	} else {
		r.appendRunEvent(ctx, record.RunID, "approval.resolved", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.toolCall.Name,
			"success":    false,
		})
	}

	if errShape := r.recordActionResultForRun(ctx, pending.message.SessionID, pending.runID, pending.message.RequestID, result); errShape != nil {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   errShape.Message,
			Error:     errShape,
		}, nil
	}
	// Carry the persisted ActionRecord governance through to the contract
	// result so the response surfaced after approval keeps the same provenance
	// as the original ToolCall (docs/03-contracts.md §3.11).
	contractResult := contractToolResult(result, GovernanceFromActionRecord(record))
	response := contracts.AgentResponse{
		RequestID:   pending.message.RequestID,
		SessionID:   pending.message.SessionID,
		Status:      contracts.AgentStatusCompleted,
		Data:        r.traceData(nil),
		ToolResults: []contracts.ToolResult{contractResult},
	}
	response.Message = approvalExecutionMessage(result, contractResult)
	if !result.Success {
		response.Status = contracts.AgentStatusFailed
		response.Error = toolErrorShape(result)
		response.Message = response.Error.Message
	}
	if errShape := r.appendAssistantTranscriptForRun(ctx, pending.message.SessionID, pending.runID, pending.message.RequestID, response.Message); errShape != nil {
		response.Status = contracts.AgentStatusFailed
		response.Error = errShape
		response.Message = errShape.Message
	}
	if response.Status == contracts.AgentStatusFailed {
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed, string(orchestration.FailureReasonToolError)); errShape != nil {
			response.Error = errShape
			response.Message = errShape.Message
		}
		response.FailureReason = string(orchestration.FailureReasonToolError)
		return response, nil
	}
	if result.Success {
		continuation := buildApprovalContinuationMessage(pending, result, r.now())
		if continuationResp, err := r.Run(ctx, continuation); err == nil {
			continuationResp.ToolResults = prependToolResultIfMissing(continuationResp.ToolResults, contractResult)
			return continuationResp, nil
		}
	}
	if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed, string(orchestration.FailureReasonToolError)); errShape != nil {
		response.Status = contracts.AgentStatusFailed
		response.Error = errShape
		response.Message = errShape.Message
	}
	response.FailureReason = string(orchestration.FailureReasonToolError)
	return response, nil
}

func (r *Runtime) responseForUnclaimedApprovedAction(record ActionRecord, pending pendingApproval) contracts.AgentResponse {
	if record.Status == ActionStatusCompleted && record.Result != nil {
		// Use the persisted ActionRecord's governance bundle so the contract
		// result mirrors the run's state at the time it was approved.
		contractResult := contractToolResult(*record.Result, GovernanceFromActionRecord(record))
		return contracts.AgentResponse{
			RequestID:   pending.message.RequestID,
			SessionID:   pending.message.SessionID,
			Status:      contracts.AgentStatusCompleted,
			Message:     approvalExecutionMessage(*record.Result, contractResult),
			Data:        r.traceData(nil),
			ToolResults: []contracts.ToolResult{contractResult},
		}
	}
	if record.Status == ActionStatusExecuting {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusBlocked,
			Message:   "Thao tác đã được xác nhận và đang được xử lý.",
			Data:      r.traceData(nil),
		}
	}
	return contracts.AgentResponse{
		RequestID: pending.message.RequestID,
		SessionID: pending.message.SessionID,
		Status:    contracts.AgentStatusFailed,
		Message:   "Thao tác xác nhận không còn ở trạng thái có thể thực thi.",
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorActionBlockedByPolicy,
			Message:   "approval action status is " + string(record.Status),
			Source:    contracts.ErrorSourcePolicy,
			Retryable: false,
		},
	}
}

func buildApprovalContinuationMessage(pending pendingApproval, result tools.ToolResult, now time.Time) contracts.UserMessage {
	var text string
	resultNote := approvalContinuationResultNote(pending.toolCall.Name)
	resultContent := truncateToolContentForLLM(result.ContentForLLM)
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
%s

Continue by calling the remaining tools in the original plan: %s
Use any resource IDs or names returned by the completed tool's result when they are needed as input for the next tool.
Call each remaining tool exactly once. Do not call a tool that already appears in the conversation history.`,
			pending.message.Text,
			pending.toolCall.Name,
			resultContent,
			resultNote,
			strings.Join(remainingNames, ", "),
		))
	} else {
		pipelineHint := ""
		if isDraftCreationTool(pending.toolCall.Name) {
			pipelineHint = "\nIf the completed tool returned a Gmail draft object like Draft.ID, use Draft.ID as the draftId argument for gmail.sendDraft to actually deliver the email."
		}
		text = strings.TrimSpace(fmt.Sprintf(`An approved tool just completed as part of the user's original request.
Luôn trả lời bằng tiếng Việt nếu người dùng đang nói tiếng Việt.

Original request:
%s

Completed tool: %s
Result: %s
%s

Check whether the original request contained additional tasks that have not yet been done.%s
If yes, call the necessary tool(s) now — do NOT ask the user again for information already given in the original request.
If all tasks are already complete, respond with a short Vietnamese summary of what was accomplished.
Do not repeat the tool that was just executed.`,
			pending.message.Text,
			pending.toolCall.Name,
			resultContent,
			resultNote,
			pipelineHint,
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

func isDraftCreationTool(toolName string) bool {
	switch toolName {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		return true
	default:
		return false
	}
}

func approvalContinuationResultNote(toolName string) string {
	if strings.TrimSpace(toolName) == "gmail.sendDraft" {
		return "Important delivery wording: gmail.sendDraft means the email was handed to Gmail for sending. Do not say the recipient received the email, do not say delivery succeeded, and avoid wording like 'sent successfully'. In Vietnamese, prefer 'Email da duoc chuyen cho Gmail de gui'."
	}
	return ""
}

func buildRevisionRequest(pending pendingApproval, comment string) string {
	input := "{}"
	if len(pending.request.ToolCall.Input) > 0 {
		if data, err := json.MarshalIndent(pending.request.ToolCall.Input, "", "  "); err == nil {
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

func approvalSummary(toolName string, riskLevel contracts.RiskLevel, arguments map[string]any) string {
	switch toolName {
	case "get_current_time":
		return "Cho phép tôi xem thời gian hiện tại nhé?"
	case "calculator":
		return "Cho phép tôi tính toán phép tính này nhé?"
	case "spawn_subtask":
		return "Cho phép tôi tạo subtask nội bộ để xử lý tiếp nhé?"
	case "filesystem.listDir":
		return "Cho phép tôi liệt kê file trong workspace nhé?"
	case "filesystem.readFile":
		return "Cho phép tôi đọc file trong workspace nhé?"
	case "filesystem.fileInfo":
		return "Cho phép tôi xem thông tin file trong workspace nhé?"
	case "filesystem.writeFile":
		return "Tôi cần bạn xác nhận trước khi ghi file trong workspace."
	case "web.search":
		return "Cho phép tôi tìm kiếm trên web nhé?"
	case "web.fetch":
		return "Cho phép tôi đọc nội dung trang web này nhé?"
	case "people.searchDirectory":
		return "Cho phép tôi tìm kiếm danh bạ Google Workspace nhé?"
	case "gmail.listEmails":
		return "Cho phép tôi xem danh sách email trong Gmail nhé?"
	case "gmail.listLabels":
		return "Cho phép tôi xem nhãn trong Gmail nhé?"
	case "gmail.getProfile":
		return "Cho phép tôi xem thông tin tài khoản Gmail nhé?"
	case "gmail.listThreads":
		return "Cho phép tôi xem danh sách thread trong Gmail nhé?"
	case "gmail.getThread":
		return "Cho phép tôi đọc nội dung thread trong Gmail nhé?"
	case "gmail.listDrafts":
		return "Cho phép tôi xem danh sách Gmail draft nhé?"
	case "gmail.getDraft":
		return "Cho phép tôi đọc nội dung Gmail draft nhé?"
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		return "Mình sẽ tạo hoặc sửa Gmail draft. Xác nhận không?"
	case "gmail.sendDraft":
		return "Mình sẽ gửi email. Xác nhận không?"
	case "gmail.deleteDraft":
		return "Mình sẽ xóa Gmail draft. Xác nhận không?"
	case "gmail.downloadAttachments":
		return "Mình sẽ tải file đính kèm về máy. Xác nhận không?"
	case "gmail.getEmail":
		return "Mình sẽ đọc nội dung email này. Xác nhận không?"
	case "gmail.modifyMessage", "gmail.batchModifyMessages":
		return "Mình sẽ sửa trạng thái hoặc nhãn Gmail. Xác nhận không?"
	case "gmail.trashMessage":
		return "Mình sẽ chuyển email vào thùng rác. Xác nhận không?"
	case "gmail.untrashMessage":
		return "Mình sẽ khôi phục email. Xác nhận không?"
	case "calendar.createEvent":
		return "Mình sẽ tạo sự kiện Calendar. Xác nhận không?"
	case "calendar.updateEvent":
		return "Mình sẽ sửa sự kiện Calendar. Xác nhận không?"
	case "calendar.respondEvent":
		return "Tôi cần bạn xác nhận trước khi phản hồi lời mời Calendar."
	case "calendar.listEvents":
		return "Cho phép tôi xem lịch Calendar nhé?"
	case "calendar.getEvent":
		return "Cho phép tôi xem chi tiết sự kiện Calendar nhé?"
	case "calendar.deleteEvent":
		return "Mình sẽ xóa sự kiện Calendar. Xác nhận không?"
	case "chat.listSpaces":
		return "Cho phép tôi xem danh sách Google Chat space nhé?"
	case "chat.listMembers":
		return "Cho phép tôi xem thành viên trong Google Chat nhé?"
	case "chat.findSpacesByMembers":
		return "Cho phép tôi tìm cuộc trò chuyện Google Chat theo thành viên nhé?"
	case "chat.listMessages":
		return "Cho phép tôi đọc tin nhắn trong Google Chat nhé?"
	case "chat.sendMessage":
		return "Mình sẽ gửi tin nhắn Google Chat. Xác nhận không?"
	case "chat.updateMessage":
		return "Mình sẽ sửa tin nhắn Google Chat. Xác nhận không?"
	case "chat.deleteMessage":
		return "Mình sẽ xóa tin nhắn Google Chat. Xác nhận không?"
	case "chat.createSpace":
		return "Mình sẽ tạo Google Chat space. Xác nhận không?"
	case "chat.addMember":
		return "Mình sẽ thêm thành viên Google Chat. Xác nhận không?"
	case "chat.removeMember":
		return "Mình sẽ xóa thành viên Google Chat. Xác nhận không?"
	case "drive.listFiles":
		return "Cho phép tôi xem danh sách file trong Google Drive nhé?"
	case "drive.getFile":
		return "Cho phép tôi xem thông tin file Google Drive nhé?"
	case "drive.exportFile":
		return "Cho phép tôi export nội dung file Google Drive nhé?"
	case "drive.downloadFile":
		return "Cho phép tôi đọc nội dung file Google Drive nhé?"
	case "drive.saveFile":
		return "Tôi cần bạn xác nhận trước khi lưu file Google Drive xuống workspace."
	case "drive.createFolder":
		return "Mình sẽ tạo folder trên Google Drive. Xác nhận không?"
	case "drive.createFile", "drive.uploadFile":
		return "Mình sẽ tạo hoặc tải file lên Google Drive. Xác nhận không?"
	case "drive.updateFileMetadata":
		return "Mình sẽ sửa metadata file Google Drive. Xác nhận không?"
	case "drive.shareFile":
		return "Mình sẽ chia sẻ file Google Drive. Xác nhận không?"
	case "drive.listPermissions":
		return "Cho phép tôi xem quyền chia sẻ file Google Drive nhé?"
	case "drive.revokePermission":
		return "Mình sẽ thu hồi quyền chia sẻ file Google Drive. Xác nhận không?"
	case "drive.moveFile", "drive.moveFiles":
		return "Mình sẽ di chuyển file hoặc folder Google Drive. Xác nhận không?"
	case "drive.trashFile":
		return "Mình sẽ chuyển file hoặc folder vào thùng rác. Xác nhận không?"
	case "drive.untrashFile":
		return "Mình sẽ khôi phục file hoặc folder Google Drive. Xác nhận không?"
	case "docs.getDocument":
		return "Cho phép tôi đọc nội dung Google Docs document nhé?"
	case "docs.createDocument":
		return "Mình sẽ tạo tài liệu Google Docs. Xác nhận không?"
	case "docs.appendText", "docs.replaceText", "docs.insertText":
		return "Mình sẽ sửa nội dung Google Docs. Xác nhận không?"
	case "docs.deleteContent":
		return "Mình sẽ xóa nội dung trong Google Docs. Xác nhận không?"
	case "sheets.getSpreadsheet":
		return "Cho phép tôi xem thông tin Google Sheets spreadsheet nhé?"
	case "sheets.readValues", "sheets.batchGetValues":
		return "Cho phép tôi đọc dữ liệu trong Google Sheets nhé?"
	case "sheets.createSpreadsheet":
		return "Mình sẽ tạo Google Sheets spreadsheet. Xác nhận không?"
	case "sheets.updateValues", "sheets.batchUpdateValues", "sheets.appendValues", "sheets.clearValues":
		return "Mình sẽ thay đổi dữ liệu trong Google Sheets. Xác nhận không?"
	case "sheets.addSheet", "sheets.renameSheet", "sheets.duplicateSheet":
		return "Mình sẽ thay đổi tab trong Google Sheets. Xác nhận không?"
	case "sheets.deleteSheet":
		return "Mình sẽ xóa tab trong Google Sheets. Xác nhận không?"
	case "sandbox.runPython":
		code, _ := arguments["code"].(string)
		return inferSandboxSummary(toolName, code)
	case "sandbox.runShell":
		code, _ := arguments["code"].(string)
		return inferSandboxSummary(toolName, code)
	default:
		return "Mình sẽ thực hiện thao tác này. Xác nhận không?"
	}
}

func inferSandboxSummary(toolName, code string) string {
	if strings.TrimSpace(code) == "" {
		if toolName == "sandbox.runShell" {
			return "Mình sẽ xử lý yêu cầu này bằng lệnh shell. Xác nhận không?"
		}
		return "Mình sẽ xử lý yêu cầu này bằng mã Python. Xác nhận không?"
	}

	hint := ""
	for _, line := range strings.Split(code, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
			continue
		}
		hint = trimmed
		break
	}
	if len(hint) > 60 {
		hint = hint[:60]
	}

	lowerHint := strings.ToLower(hint)
	lowerCode := strings.ToLower(code)
	text := lowerHint + "\n" + lowerCode

	outcome := "xử lý dữ liệu"
	switch {
	case strings.Contains(text, "open(") || strings.Contains(text, "fitz") || strings.Contains(text, ".pdf"):
		outcome = "đọc nội dung file PDF"
	case strings.Contains(text, "csv") || strings.Contains(text, "pandas"):
		outcome = "xử lý dữ liệu từ file"
	case strings.Contains(text, "requests") || strings.Contains(text, "http"):
		outcome = "gọi API bên ngoài"
	}

	if toolName == "sandbox.runShell" {
		return "Mình sẽ " + outcome + " bằng lệnh shell. Xác nhận không?"
	}
	return "Mình sẽ " + outcome + " bằng mã Python. Xác nhận không?"
}

func approvalExecutionMessage(result tools.ToolResult, contractResult contracts.ToolResult) string {
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
