package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
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
		}
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed); errShape != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
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
		if pending.actionID == "" {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   "Không tìm thấy action tương ứng với approval.",
				Error:     internalError("approval action id is missing", contracts.ErrorSourceAgent),
			}, nil
		}
		if _, err := r.stateStore.MarkActionApproved(ctx, pending.actionID); err != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   "Không thể cập nhật approval.",
				Error:     internalError("approve action: "+err.Error(), contracts.ErrorSourceAgent),
			}, nil
		}
		r.appendRunEvent(ctx, pending.runID, "approval_approved", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.request.ToolCall.ToolName,
		})
		r.recordApprovalObservation(ActionStatusApproved)
		return r.resumeApprovedAction(ctx, pending)
	case contracts.ApprovalDecisionRejected:
		if pending.actionID != "" {
			if _, err := r.stateStore.MarkActionRejected(ctx, pending.actionID); err != nil && !errors.Is(err, ErrRuntimeStateNotFound) {
				return contracts.AgentResponse{
					RequestID: pending.message.RequestID,
					SessionID: pending.message.SessionID,
					Status:    contracts.AgentStatusFailed,
					Message:   "Không thể cập nhật approval bị từ chối.",
					Error:     internalError("reject action: "+err.Error(), contracts.ErrorSourceAgent),
				}, nil
			}
			r.recordApprovalObservation(ActionStatusRejected)
		}
		r.appendRunEvent(ctx, pending.runID, "approval_rejected", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.request.ToolCall.ToolName,
			"comment":    strings.TrimSpace(decision.Comment),
		})
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusBlocked); errShape != nil {
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
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusBlocked,
			Message:   "Đã hủy thao tác. Tôi chưa thực hiện tool nào.",
			Data:      r.traceData(nil),
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
		}
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed); errShape != nil {
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
	contractCall := contracts.ToolCall{
		ToolCallID: toolCall.ID,
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolName:   toolCall.Name,
		Input:      input,
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
	}
}

func enrichApprovalInput(toolName string, input map[string]any, transcript []providers.Message) map[string]any {
	switch strings.TrimSpace(toolName) {
	case "drive.moveFile", "drive.moveFiles":
		return enrichDriveMoveApprovalInput(input, transcript)
	default:
		return input
	}
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
	request := contracts.ApprovalRequest{
		ApprovalID: record.ApprovalID,
		RequestID:  record.RequestID,
		SessionID:  record.SessionID,
		ToolCallID: record.ToolCallID,
		Status:     approvalStatusForAction(record.Status),
		RiskLevel:  record.RiskLevel,
		Summary:    approvalSummary(record.ToolName, record.RiskLevel),
		ToolCall: contracts.ToolCall{
			ToolCallID: record.ToolCallID,
			RequestID:  record.RequestID,
			SessionID:  record.SessionID,
			ToolName:   record.ToolName,
			Input:      cloneArguments(record.ArgsSnapshot),
		},
		CreatedAt: record.CreatedAt,
		ExpiresAt: record.ApprovalExpiresAt,
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
	switch status {
	case ActionStatusPendingApproval, ActionStatusApproved, ActionStatusExecuting, ActionStatusCompleted:
		return true
	default:
		return false
	}
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
	if errShape := r.recordRuntimeToolCall(ctx, record.RunID, pending.toolCall, result, time.Since(startedAt)); errShape != nil {
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
		r.appendRunEvent(ctx, record.RunID, "approval_executed", map[string]any{
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
		r.appendRunEvent(ctx, record.RunID, "approval_executed", map[string]any{
			"approvalId": pending.request.ApprovalID,
			"toolName":   pending.toolCall.Name,
			"success":    false,
		})
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
	contractResult := contractToolResult(result)
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
	if errShape := r.appendAssistantTranscript(ctx, pending.message.SessionID, response.Message); errShape != nil {
		response.Status = contracts.AgentStatusFailed
		response.Error = errShape
		response.Message = errShape.Message
	}
	if response.Status == contracts.AgentStatusFailed {
		if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed); errShape != nil {
			response.Error = errShape
			response.Message = errShape.Message
		}
		return response, nil
	}
	if result.Success {
		continuation := buildApprovalContinuationMessage(pending, result, r.now())
		if continuationResp, err := r.Run(ctx, continuation); err == nil {
			continuationResp.ToolResults = prependToolResultIfMissing(continuationResp.ToolResults, contractResult)
			return continuationResp, nil
		}
	}
	if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed); errShape != nil {
		response.Status = contracts.AgentStatusFailed
		response.Error = errShape
		response.Message = errShape.Message
	}
	return response, nil
}

func (r *Runtime) responseForUnclaimedApprovedAction(record ActionRecord, pending pendingApproval) contracts.AgentResponse {
	if record.Status == ActionStatusCompleted && record.Result != nil {
		contractResult := contractToolResult(*record.Result)
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
			result.ContentForLLM,
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
			result.ContentForLLM,
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

func approvalSummary(toolName string, riskLevel contracts.RiskLevel) string {
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
	case "drive.createFolder":
		return "Tôi cần bạn xác nhận trước khi tạo folder trên Google Drive."
	case "drive.createFile", "drive.uploadFile":
		return "Tôi cần bạn xác nhận trước khi tạo hoặc upload file lên Google Drive."
	case "drive.updateFileMetadata":
		return "Tôi cần bạn xác nhận trước khi sửa metadata file Google Drive."
	case "drive.shareFile":
		return "Tôi cần bạn xác nhận trước khi chia sẻ file Google Drive."
	case "drive.revokePermission":
		return "Tôi cần bạn xác nhận trước khi thu hồi quyền chia sẻ file Google Drive."
	case "drive.moveFile", "drive.moveFiles":
		return "Tôi cần bạn xác nhận trước khi di chuyển file hoặc folder Google Drive."
	case "drive.trashFile":
		return "Tôi cần bạn xác nhận trước khi chuyển file hoặc folder Google Drive vào thùng rác."
	case "drive.untrashFile":
		return "Tôi cần bạn xác nhận trước khi khôi phục file hoặc folder Google Drive."
	case "docs.createDocument":
		return "Tôi cần bạn xác nhận trước khi tạo Google Docs document."
	case "docs.appendText", "docs.replaceText", "docs.insertText":
		return "Tôi cần bạn xác nhận trước khi sửa nội dung Google Docs document."
	case "docs.deleteContent":
		return "Tôi cần bạn xác nhận trước khi xóa nội dung trong Google Docs document."
	case "sheets.createSpreadsheet":
		return "Tôi cần bạn xác nhận trước khi tạo Google Sheets spreadsheet."
	case "sheets.updateValues", "sheets.batchUpdateValues", "sheets.appendValues", "sheets.clearValues":
		return "Tôi cần bạn xác nhận trước khi thay đổi dữ liệu trong Google Sheets."
	case "sheets.addSheet", "sheets.renameSheet", "sheets.duplicateSheet":
		return "Tôi cần bạn xác nhận trước khi thay đổi tab trong Google Sheets."
	case "sheets.deleteSheet":
		return "Tôi cần bạn xác nhận trước khi xóa tab trong Google Sheets."
	case "sandbox.runPython", "sandbox.runShell":
		return "Tôi cần bạn xác nhận trước khi chạy code hoặc lệnh trong sandbox."
	default:
		return fmt.Sprintf("Tôi cần bạn xác nhận trước khi chạy %s vì risk là %s.", toolName, riskLevel)
	}
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
