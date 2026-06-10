package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Capability string

const (
	CapabilityReadOnly Capability = "read_only"
	CapabilityMutating Capability = "mutating"
)

type RiskLevel string

const (
	RiskLevelSafeRead      RiskLevel = "safe_read"
	RiskLevelSafeCompute   RiskLevel = "safe_compute"
	RiskLevelSensitiveRead RiskLevel = "sensitive_read"
	RiskLevelExternalWrite RiskLevel = "external_write"
	RiskLevelLocalWrite    RiskLevel = "local_write"
	RiskLevelCodeExecution RiskLevel = "code_execution"
	RiskLevelDestructive   RiskLevel = "destructive"
)

type ToolSchema map[string]any

type Tool interface {
	Name() string
	Description() string
	Parameters() ToolSchema
	Capability() Capability
	RiskLevel() RiskLevel
	Execute(ctx context.Context, call ToolCall) ToolResult
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ToolResult struct {
	ToolCallID     string
	ToolName       string
	Success        bool
	ContentForLLM  string
	ContentForUser string
	Data           any
	ArtifactRef    *ArtifactRef
	SourceRefs     []SourceRef
	Metadata       map[string]any
	Error          *ToolError
}

type ArtifactRef struct {
	Kind  string
	Label string
	URI   string
	ID    string
	Meta  map[string]any
}

type SourceRef struct {
	Kind     string
	Label    string
	URI      string
	ID       string
	MimeType string
	Meta     map[string]any
}

type ToolError struct {
	Code    string
	Message string
}

const (
	defaultLLMContentLimit  = 20000
	defaultUserContentLimit = 12000
)

var sensitiveOutputPattern = regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[a-z0-9._\-]+|(bearer\s+)[a-z0-9._\-]+|(refresh[_ -]?token["'\s:=]+)[^"',\s]+|(access[_ -]?token["'\s:=]+)[^"',\s]+|(client[_ -]?secret["'\s:=]+)[^"',\s]+|(api[_ -]?key["'\s:=]+)[^"',\s]+|(sk-[a-z0-9]{8,})|(xox[baprs]-[a-z0-9-]+)`)

const (
	ErrorToolNotFound         = "TOOL_NOT_FOUND"
	ErrorInvalidArgument      = "TOOL_INPUT_INVALID"
	ErrorExecutionFailed      = "INTERNAL_ERROR"
	ErrorBlockedByPolicy      = "ACTION_BLOCKED_BY_POLICY"
	ErrorTimeout              = "PROVIDER_TIMEOUT"
	ErrorMaxIterationsReached = "INTERNAL_ERROR"
)

func ToolNotFoundResult(call ToolCall) ToolResult {
	return ErrorResult(call, ErrorToolNotFound, "tool not found: "+call.Name, WithLLMContent("Tool not found: "+call.Name), WithUserContent("Không tìm thấy tool: "+call.Name))
}

func PermissionDeniedResult(call ToolCall) ToolResult {
	return ErrorResult(call, ErrorBlockedByPolicy, "tool blocked by policy: "+call.Name, WithLLMContent("Permission denied for tool: "+call.Name), WithUserContent("Không có quyền dùng tool: "+call.Name))
}

func ExecutionErrorResult(call ToolCall, err error) ToolResult {
	message := "tool execution failed"
	if err != nil {
		message = err.Error()
	}

	return ErrorResult(call, ErrorExecutionFailed, message, WithLLMContent(fmt.Sprintf("Tool execution error for %s: %s", call.Name, message)), WithUserContent("Tool lỗi khi chạy: "+call.Name))
}

type ResultOption func(*resultOptions)

type resultOptions struct {
	contentForLLM  string
	contentForUser string
	data           any
	artifactRef    *ArtifactRef
	sourceRefs     []SourceRef
	metadata       map[string]any
	llmLimit       int
	userLimit      int
}

func SuccessResult(call ToolCall, output any, options ...ResultOption) ToolResult {
	opts := applyResultOptions(options...)
	content := renderJSON(output)
	llmContent := firstNonEmpty(opts.contentForLLM, content)
	userContent := firstNonEmpty(opts.contentForUser, content)
	data := opts.data
	if data == nil {
		data = output
	}
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  sanitizeToolContent(llmContent, opts.llmLimit),
		ContentForUser: sanitizeToolContent(userContent, opts.userLimit),
		Data:           data,
		ArtifactRef:    opts.artifactRef,
		SourceRefs:     cloneSourceRefs(opts.sourceRefs),
		Metadata:       cloneMap(opts.metadata),
	}
}

func ErrorResult(call ToolCall, code string, message string, options ...ResultOption) ToolResult {
	opts := applyResultOptions(options...)
	if strings.TrimSpace(code) == "" {
		code = ErrorExecutionFailed
	}
	if strings.TrimSpace(message) == "" {
		message = "tool execution failed"
	}
	llmContent := firstNonEmpty(opts.contentForLLM, code+": "+message)
	userContent := firstNonEmpty(opts.contentForUser, message)
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  sanitizeToolContent(llmContent, opts.llmLimit),
		ContentForUser: sanitizeToolContent(userContent, opts.userLimit),
		Data:           opts.data,
		ArtifactRef:    opts.artifactRef,
		SourceRefs:     cloneSourceRefs(opts.sourceRefs),
		Metadata:       cloneMap(opts.metadata),
		Error:          &ToolError{Code: code, Message: sanitizeToolContent(message, opts.userLimit)},
	}
}

func WithLLMContent(content string) ResultOption {
	return func(opts *resultOptions) { opts.contentForLLM = content }
}

func WithUserContent(content string) ResultOption {
	return func(opts *resultOptions) { opts.contentForUser = content }
}

func WithData(data any) ResultOption {
	return func(opts *resultOptions) { opts.data = data }
}

func WithArtifactRef(ref ArtifactRef) ResultOption {
	return func(opts *resultOptions) { opts.artifactRef = &ref }
}

func WithSourceRefs(refs ...SourceRef) ResultOption {
	return func(opts *resultOptions) { opts.sourceRefs = append(opts.sourceRefs, refs...) }
}

func WithMetadata(metadata map[string]any) ResultOption {
	return func(opts *resultOptions) { opts.metadata = metadata }
}

func WithContentLimits(llmLimit int, userLimit int) ResultOption {
	return func(opts *resultOptions) {
		opts.llmLimit = llmLimit
		opts.userLimit = userLimit
	}
}

func SanitizeContent(content string, limit int) string {
	return sanitizeToolContent(content, limit)
}

func applyResultOptions(options ...ResultOption) resultOptions {
	opts := resultOptions{llmLimit: defaultLLMContentLimit, userLimit: defaultUserContentLimit}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if opts.llmLimit <= 0 {
		opts.llmLimit = defaultLLMContentLimit
	}
	if opts.userLimit <= 0 {
		opts.userLimit = defaultUserContentLimit
	}
	return opts
}

func renderJSON(output any) string {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%#v", output)
	}
	return string(data)
}

func sanitizeToolContent(content string, limit int) string {
	content = strings.TrimSpace(sensitiveOutputPattern.ReplaceAllString(content, "${1}${2}${3}${4}${5}${6}[REDACTED]"))
	if content == "" {
		return ""
	}
	runes := []rune(content)
	if limit > 0 && len(runes) > limit {
		return strings.TrimSpace(string(runes[:limit])) + "\n[truncated]"
	}
	return content
}

func cloneSourceRefs(refs []SourceRef) []SourceRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]SourceRef, len(refs))
	copy(out, refs)
	for i := range out {
		out[i].Meta = cloneMap(out[i].Meta)
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
