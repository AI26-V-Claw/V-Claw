package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/policies"
	"vclaw/internal/tools"
)

const (
	SubtaskToolName = "spawn_subtask"

	defaultSubtaskTimeout   = 300 * time.Second
	maxSubtaskTimeout       = 600 * time.Second
	subtaskCountTTL         = time.Hour
	defaultSubtaskMaxDepth  = 1
	subtaskRoleLeaf         = "leaf"
	subtaskRoleOrchestrator = "orchestrator"
)

type parentRunContextKey struct{}
type subtaskDepthContextKey struct{}

func withParentRunID(ctx context.Context, parentRunID string) context.Context {
	if strings.TrimSpace(parentRunID) == "" {
		return ctx
	}
	return context.WithValue(ctx, parentRunContextKey{}, parentRunID)
}

func parentRunIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(parentRunContextKey{}).(string)
	return strings.TrimSpace(value)
}

func withSubtaskDepth(ctx context.Context, depth int) context.Context {
	if depth <= 0 {
		return ctx
	}
	return context.WithValue(ctx, subtaskDepthContextKey{}, depth)
}

func subtaskDepthFromContext(ctx context.Context) int {
	value, _ := ctx.Value(subtaskDepthContextKey{}).(int)
	if value < 0 {
		return 0
	}
	return value
}

type subtaskCoordinator struct {
	mu                sync.Mutex
	maxChildrenPerRun int
	ttl               time.Duration
	now               func() time.Time
	counts            map[string]subtaskRunCount
}

type subtaskRunCount struct {
	count    int
	lastSeen time.Time
}

func newSubtaskCoordinator(maxChildrenPerRun int) *subtaskCoordinator {
	if maxChildrenPerRun <= 0 {
		maxChildrenPerRun = 4
	}
	return &subtaskCoordinator{maxChildrenPerRun: maxChildrenPerRun, ttl: subtaskCountTTL, now: time.Now, counts: make(map[string]subtaskRunCount)}
}

func (c *subtaskCoordinator) reserve(parentRunID string) (int, error) {
	if c == nil {
		return 0, fmt.Errorf("subtask coordinator is not configured")
	}
	parentRunID = strings.TrimSpace(parentRunID)
	if parentRunID == "" {
		return 0, fmt.Errorf("parent_run_id is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.currentTime()
	c.pruneExpiredLocked(now)
	entry := c.counts[parentRunID]
	if entry.count >= c.maxChildrenPerRun {
		return entry.count, fmt.Errorf("max children per parent run exceeded: %d", c.maxChildrenPerRun)
	}
	entry.count++
	entry.lastSeen = now
	c.counts[parentRunID] = entry
	return entry.count, nil
}

func (c *subtaskCoordinator) complete(parentRunID string) {
	if c == nil {
		return
	}
	parentRunID = strings.TrimSpace(parentRunID)
	if parentRunID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.counts, parentRunID)
}

func (c *subtaskCoordinator) currentTime() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

func (c *subtaskCoordinator) pruneExpiredLocked(now time.Time) {
	if c.ttl <= 0 {
		return
	}
	for parentRunID, entry := range c.counts {
		if !entry.lastSeen.IsZero() && now.Sub(entry.lastSeen) > c.ttl {
			delete(c.counts, parentRunID)
		}
	}
}

type SubtaskTool struct {
	parent *Runtime
}

func NewSubtaskTool(parent *Runtime) *SubtaskTool {
	return &SubtaskTool{parent: parent}
}

func (*SubtaskTool) Name() string { return SubtaskToolName }

func (*SubtaskTool) Description() string {
	return "Spawn a temporary task-scoped subagent with isolated context and explicit allowed_skills or allowed_tool_groups. Defaults to sync leaf mode."
}

func (*SubtaskTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{"type": "string", "description": "Specific subtask for the temporary child agent."},
			"allowed_skills": map[string]any{
				"type":        "array",
				"description": "High-level skill profile names to grant to the child agent. At least one of allowed_skills or allowed_tool_groups is required.",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
			},
			"allowed_tool_groups": map[string]any{
				"type":        "array",
				"description": "Tool registry groups to grant to the child agent, filtered by policy and safety. At least one of allowed_skills or allowed_tool_groups is required.",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
			},
			"mode":            map[string]any{"type": "string", "enum": []string{"sync"}, "description": "sync waits for the child result."},
			"timeout_seconds": map[string]any{"type": "integer", "description": "Child deadline in seconds. Defaults to 300 and is capped at 600."},
			"label":           map[string]any{"type": "string", "description": "Optional human-readable subtask label."},
			"model":           map[string]any{"type": "string", "description": "Optional model override for the child runtime."},
			"role":            map[string]any{"type": "string", "enum": []string{subtaskRoleLeaf, subtaskRoleOrchestrator}, "description": "Child role. leaf cannot delegate; orchestrator may delegate while depth budget remains."},
			"context":         map[string]any{"type": "string", "description": "Optional parent-provided context for the isolated child prompt."},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	}
}

func (*SubtaskTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (*SubtaskTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }

func (t *SubtaskTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	startedAt := time.Now()
	if t == nil || t.parent == nil || t.parent.registry == nil || t.parent.provider == nil {
		return subtaskErrorResult(call, "runtime_unavailable", "parent runtime is not available", startedAt)
	}
	request, err := parseSubtaskRequestWithLimits(call.Arguments, t.parent.subtaskDefaultTimeout, t.parent.subtaskMaxTimeout)
	if err != nil {
		return subtaskErrorResult(call, "invalid_request", err.Error(), startedAt)
	}
	parentRunID := parentRunIDFromContext(ctx)
	if parentRunID == "" {
		parentRunID = "run_" + randomHex(8)
	}
	parentDepth := subtaskDepthFromContext(ctx)
	childDepth := parentDepth + 1
	if childDepth > t.parent.subtaskMaxDepth {
		return subtaskErrorResult(call, "max_depth_exceeded", fmt.Sprintf("max subtask depth exceeded: %d", t.parent.subtaskMaxDepth), startedAt)
	}
	childRegistry, effectiveTools, err := buildSubtaskRegistry(t.parent, request, childDepth)
	if err != nil {
		return subtaskErrorResult(call, "capability_rejected", err.Error(), startedAt)
	}
	childNumber, err := t.parent.subtasks.reserve(parentRunID)
	if err != nil {
		return subtaskErrorResult(call, "max_children_exceeded", err.Error(), startedAt)
	}
	taskID := fmt.Sprintf("subtask_%s_%d", safeID(parentRunID), childNumber)
	t.parent.appendRunEvent(ctx, parentRunID, "subtask.started", map[string]any{
		"task_id":         taskID,
		"label":           request.Label,
		"role":            request.Role,
		"depth":           childDepth,
		"timeout_seconds": int(request.Timeout.Seconds()),
		"effective_tools": effectiveTools,
	})
	childResult := t.runChild(ctx, taskID, parentRunID, childDepth, request, childRegistry, effectiveTools, startedAt)
	eventType := "subtask.completed"
	if childResult.Status == "timeout" {
		eventType = "subtask.timeout"
	} else if childResult.Status == "failed" || childResult.Error != "" {
		eventType = "subtask.failed"
	}
	t.parent.appendRunEvent(ctx, parentRunID, eventType, map[string]any{
		"task_id":      taskID,
		"label":        request.Label,
		"status":       childResult.Status,
		"runtime_ms":   childResult.RuntimeMS,
		"timed_out":    childResult.TimedOut,
		"error":        childResult.Error,
		"child_run_id": "run_" + safeID(taskID),
	})
	return subtaskSuccessResult(call, childResult, startedAt)
}

type subtaskRequest struct {
	Task              string
	Context           string
	AllowedSkills     []string
	AllowedToolGroups []string
	Mode              string
	Timeout           time.Duration
	Label             string
	Model             string
	Role              string
}

type subtaskResult struct {
	TaskID     string         `json:"task_id"`
	Label      string         `json:"label,omitempty"`
	Status     string         `json:"status"`
	Content    string         `json:"content,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	Error      string         `json:"error,omitempty"`
	RuntimeMS  int64          `json:"runtime_ms"`
	Iterations int            `json:"iterations"`
	TimedOut   bool           `json:"timed_out"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func parseSubtaskRequest(args map[string]any) (subtaskRequest, error) {
	return parseSubtaskRequestWithLimits(args, defaultSubtaskTimeout, maxSubtaskTimeout)
}

func parseSubtaskRequestWithLimits(args map[string]any, defaultTimeout time.Duration, maxTimeout time.Duration) (subtaskRequest, error) {
	if defaultTimeout <= 0 {
		defaultTimeout = defaultSubtaskTimeout
	}
	if maxTimeout <= 0 {
		maxTimeout = maxSubtaskTimeout
	}
	if defaultTimeout > maxTimeout {
		defaultTimeout = maxTimeout
	}
	request := subtaskRequest{Mode: "sync", Timeout: defaultTimeout, Role: subtaskRoleLeaf}
	request.Task = strings.TrimSpace(subtaskStringArg(args, "task"))
	if request.Task == "" {
		return request, fmt.Errorf("task is required")
	}
	request.Context = strings.TrimSpace(subtaskStringArg(args, "context"))
	request.Label = strings.TrimSpace(subtaskStringArg(args, "label"))
	request.Model = strings.TrimSpace(subtaskStringArg(args, "model"))
	if mode := strings.TrimSpace(subtaskStringArg(args, "mode")); mode != "" {
		if mode != "sync" {
			return request, fmt.Errorf("mode must be sync")
		}
		request.Mode = mode
	}
	if role := strings.TrimSpace(subtaskStringArg(args, "role")); role != "" {
		if role != subtaskRoleLeaf && role != subtaskRoleOrchestrator {
			return request, fmt.Errorf("role must be leaf or orchestrator")
		}
		request.Role = role
	}
	if seconds := subtaskIntArg(args, "timeout_seconds"); seconds > 0 {
		request.Timeout = time.Duration(seconds) * time.Second
	}
	if request.Timeout > maxTimeout {
		request.Timeout = maxTimeout
	}
	request.AllowedSkills = subtaskStringSliceArg(args, "allowed_skills")
	request.AllowedToolGroups = subtaskStringSliceArg(args, "allowed_tool_groups")
	if len(request.AllowedSkills) == 0 && len(request.AllowedToolGroups) == 0 {
		return request, fmt.Errorf("allowed_skills or allowed_tool_groups is required")
	}
	return request, nil
}

func (t *SubtaskTool) runChild(ctx context.Context, taskID string, parentRunID string, childDepth int, request subtaskRequest, registry *tools.ToolRegistry, effectiveTools []string, startedAt time.Time) subtaskResult {
	childCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	childCtx = withParentRunID(childCtx, parentRunID)
	childCtx = withSubtaskDepth(childCtx, childDepth)
	childSessionID := taskID + "_session"
	childRequestID := taskID + "_request"
	model := t.parent.model
	if request.Model != "" {
		model = request.Model
	}
	child := NewRuntime(RuntimeConfig{
		Provider:                   t.parent.provider,
		Registry:                   registry,
		Observer:                   t.parent.observer,
		ReferenceResolver:          nil,
		Policy:                     policies.NewToolPolicy(),
		SessionStore:               t.parent.sessionStore,
		StateStore:                 t.parent.stateStore,
		Logger:                     t.parent.logger,
		ToolHooks:                  t.parent.toolHooks,
		IterationBudget:            t.parent.iterationBudgetLimit,
		ToolTimeout:                t.parent.toolTimeout,
		ParallelExecutionEnabled:   t.parent.parallelExecutionEnabled,
		ParallelMaxWorkers:         t.parent.parallelMaxWorkers,
		ParallelToolTimeoutDefault: t.parent.parallelToolTimeoutDefault,
		Model:                      model,
		Now:                        t.parent.now,
		Compactor:                  nil,
		ContextWindow:              t.parent.contextWindow,
		ContextBudget:              t.parent.contextBudget,
		MemoryClassifierModel:      t.parent.memoryClassifierModel,
		SubtaskMaxChildren:         t.parent.subtasks.maxChildrenPerRun,
		SubtaskMaxDepth:            t.parent.subtaskMaxDepth,
		SubtaskDefaultTimeout:      t.parent.subtaskDefaultTimeout,
		SubtaskMaxTimeout:          t.parent.subtaskMaxTimeout,
		ProviderTimeout:            request.Timeout,
	})
	child.subtasks = t.parent.subtasks
	response, err := child.Run(childCtx, contracts.UserMessage{
		RequestID: childRequestID,
		SessionID: childSessionID,
		Channel:   "subtask",
		Text:      childPrompt(request),
		Timestamp: t.parent.now(),
		Metadata: map[string]any{
			"runId":         taskID,
			"parent_run_id": parentRunID,
			"role":          request.Role,
			"depth":         childDepth,
			"max_depth":     t.parent.subtaskMaxDepth,
		},
	})
	runtimeMS := time.Since(startedAt).Milliseconds()
	result := subtaskResult{TaskID: taskID, Label: request.Label, RuntimeMS: runtimeMS, Metadata: map[string]any{
		"parent_run_id":   parentRunID,
		"role":            request.Role,
		"depth":           childDepth,
		"max_depth":       t.parent.subtaskMaxDepth,
		"effective_tools": effectiveTools,
	}}
	if childCtx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Error = childCtx.Err().Error()
		result.TimedOut = true
		return result
	}
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result
	}
	result.Status = string(response.Status)
	result.Content = response.Message
	result.Summary = response.Message
	if response.Error != nil {
		result.Error = response.Error.Message
	}
	return result
}

func childPrompt(request subtaskRequest) string {
	roleInstruction := "Do not delegate, spawn other agents, message teams, or perform actions outside your visible tools."
	if request.Role == subtaskRoleOrchestrator {
		roleInstruction = "You may delegate only when the visible tools allow it and only within the remaining depth budget."
	}
	contextBlock := ""
	if request.Context != "" {
		contextBlock = "\n\nParent-provided context:\n" + request.Context
	}
	return strings.TrimSpace(`You are a temporary ` + request.Role + ` subagent. Complete only the delegated task below.
` + roleInstruction + `
Return a concise summary/result for the parent agent.
` + contextBlock + `

Delegated task:
` + request.Task)
}

func buildSubtaskRegistry(parent *Runtime, request subtaskRequest, childDepth int) (*tools.ToolRegistry, []string, error) {
	requested, err := resolveAllowedTools(parent.registry, request.AllowedSkills, request.AllowedToolGroups)
	if err != nil {
		return nil, nil, err
	}
	deny := concreteSubtaskDenyTools()
	childRegistry := tools.NewToolRegistry()
	effective := make([]string, 0, len(requested))
	for name := range requested {
		if deny[name] && !allowDelegationTool(name, request.Role, childDepth, parent.subtaskMaxDepth) {
			continue
		}
		definition, found := parent.registry.GetDefinition(name)
		if !found || !definition.Enabled {
			continue
		}
		if parent.policy.DecideToolCall("subtask_capability", definition, true, parent.now()).Decision != contracts.RiskDecisionAllow {
			continue
		}
		if !subtaskRoleAllows(definition, request.Role, childDepth, parent.subtaskMaxDepth) {
			continue
		}
		tool, ok := parent.registry.GetTool(name)
		if !ok || tool == nil {
			continue
		}
		if err := childRegistry.RegisterWithEntry(tool, tools.ToolRegistryEntry{
			Name:             definition.Name,
			Owner:            definition.Owner,
			Group:            definition.Group,
			Description:      definition.Description,
			Parameters:       definition.Parameters,
			Capability:       definition.Capability,
			RiskLevel:        definition.RiskLevel,
			RequiresApproval: definition.RequiresApproval,
			Timeout:          definition.Timeout,
			Enabled:          definition.Enabled,
		}); err != nil {
			return nil, nil, err
		}
		effective = append(effective, name)
	}
	if len(effective) == 0 {
		return nil, nil, fmt.Errorf("allowed capabilities resolved to no usable tools")
	}
	return childRegistry, effective, nil
}

func resolveAllowedTools(registry *tools.ToolRegistry, skills []string, groups []string) (map[string]struct{}, error) {
	resolved, err := resolveAllowedSkills(skills)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		definitions := registry.ListToolsByGroup(group)
		if len(definitions) == 0 {
			return nil, fmt.Errorf("unknown or empty tool group: %s", group)
		}
		for _, definition := range definitions {
			resolved[definition.Name] = struct{}{}
		}
	}
	return resolved, nil
}

func resolveAllowedSkills(skills []string) (map[string]struct{}, error) {
	registry := map[string][]string{
		"basic_compute": {"get_current_time", "calculator"},
		"code_reading":  {"filesystem.listDir", "filesystem.readFile", "filesystem.fileInfo"},
		"repo_audit":    {"filesystem.listDir", "filesystem.readFile", "filesystem.fileInfo"},
		"web_research":  {"web.search", "web.fetch"},
		"workspace_read": {
			"gmail.listEmails", "gmail.listLabels", "gmail.getProfile", "gmail.getEmail", "gmail.listThreads", "gmail.getThread", "gmail.listDrafts", "gmail.getDraft",
			"calendar.listEvents",
			"chat.listSpaces", "chat.listMembers", "chat.findSpacesByMembers", "chat.listMessages",
			"drive.listFiles", "drive.getFile", "drive.exportFile", "drive.downloadFile", "drive.listPermissions",
			"docs.getDocument",
			"sheets.getSpreadsheet", "sheets.readValues", "sheets.batchGetValues",
			"people.searchDirectory",
		},
	}
	resolved := make(map[string]struct{})
	for _, skill := range skills {
		skill = strings.TrimSpace(skill)
		toolsForSkill, ok := registry[skill]
		if !ok {
			return nil, fmt.Errorf("unknown skill: %s", skill)
		}
		for _, name := range toolsForSkill {
			resolved[name] = struct{}{}
		}
	}
	return resolved, nil
}

func concreteSubtaskDenyTools() map[string]bool {
	return map[string]bool{
		SubtaskToolName:             true,
		"chat.sendMessage":          true,
		"chat.updateMessage":        true,
		"chat.deleteMessage":        true,
		"chat.createSpace":          true,
		"chat.addMember":            true,
		"chat.removeMember":         true,
		"sandbox.runPython":         true,
		"sandbox.runShell":          true,
		"filesystem.writeFile":      true,
		"gmail.createDraft":         true,
		"gmail.updateDraft":         true,
		"gmail.sendDraft":           true,
		"gmail.deleteDraft":         true,
		"gmail.replyDraft":          true,
		"gmail.forwardDraft":        true,
		"gmail.downloadAttachments": true,
		"gmail.modifyMessage":       true,
		"gmail.batchModifyMessages": true,
		"gmail.trashMessage":        true,
		"gmail.untrashMessage":      true,
		"calendar.createEvent":      true,
		"calendar.updateEvent":      true,
		"calendar.respondEvent":     true,
		"calendar.deleteEvent":      true,
		"drive.createFolder":        true,
		"drive.createFile":          true,
		"drive.uploadFile":          true,
		"drive.updateFileMetadata":  true,
		"drive.shareFile":           true,
		"drive.revokePermission":    true,
		"drive.moveFile":            true,
		"drive.moveFiles":           true,
		"drive.trashFile":           true,
		"drive.untrashFile":         true,
		"docs.createDocument":       true,
		"docs.appendText":           true,
		"docs.replaceText":          true,
		"docs.insertText":           true,
		"docs.deleteContent":        true,
		"sheets.createSpreadsheet":  true,
		"sheets.updateValues":       true,
		"sheets.batchUpdateValues":  true,
		"sheets.appendValues":       true,
		"sheets.clearValues":        true,
		"sheets.addSheet":           true,
		"sheets.renameSheet":        true,
		"sheets.deleteSheet":        true,
		"sheets.duplicateSheet":     true,
	}
}

func leafRoleAllows(definition tools.ToolDefinition) bool {
	return definition.Capability == tools.CapabilityReadOnly &&
		(definition.RiskLevel == tools.RiskLevelSafeRead || definition.RiskLevel == tools.RiskLevelSafeCompute)
}

func subtaskRoleAllows(definition tools.ToolDefinition, role string, childDepth int, maxDepth int) bool {
	if allowDelegationTool(definition.Name, role, childDepth, maxDepth) {
		return true
	}
	return leafRoleAllows(definition)
}

func allowDelegationTool(name string, role string, childDepth int, maxDepth int) bool {
	return name == SubtaskToolName && role == subtaskRoleOrchestrator && childDepth < maxDepth
}

func subtaskSuccessResult(call tools.ToolCall, result subtaskResult, startedAt time.Time) tools.ToolResult {
	result.RuntimeMS = time.Since(startedAt).Milliseconds()
	data, _ := json.Marshal(result)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  string(data),
		ContentForUser: string(data),
		Metadata:       map[string]any{"task_id": result.TaskID, "status": result.Status, "runtime_ms": result.RuntimeMS},
	}
}

func subtaskErrorResult(call tools.ToolCall, code string, message string, startedAt time.Time) tools.ToolResult {
	result := subtaskResult{Status: "failed", Error: message, RuntimeMS: time.Since(startedAt).Milliseconds()}
	data, _ := json.Marshal(result)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  string(data),
		ContentForUser: message,
		Error:          &tools.ToolError{Code: tools.ErrorInvalidArgument, Message: code + ": " + message},
		Metadata:       map[string]any{"status": "failed", "runtime_ms": result.RuntimeMS},
	}
}

func subtaskStringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

func subtaskIntArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func subtaskStringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if strings, ok := raw.([]string); ok {
			return strings
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
