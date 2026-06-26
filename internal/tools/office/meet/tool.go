package meet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	googleconnector "vclaw/internal/connectors/google"
	"vclaw/internal/connectors/google/common"
	gmeet "vclaw/internal/connectors/google/meet"
	"vclaw/internal/tools"
	"vclaw/internal/tools/office"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameCreateMeeting = "meet.createMeeting"

	ModeForLater = "for_later"
	ModeInstant  = "instant"
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{
		Name:             ToolNameCreateMeeting,
		Owner:            "integration",
		Description:      "Create a standalone Google Meet meeting space for later use or immediate sharing.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
}

type Connector interface {
	CreateSpace(ctx context.Context) (gmeet.Space, error)
}

type Service struct {
	connector Connector
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ErrorShape) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type CreateMeetingInput struct {
	Mode string
}

type CreateMeetingOutput struct {
	SpaceName   string `json:"spaceName"`
	MeetingURI  string `json:"meetingUri"`
	MeetingCode string `json:"meetingCode,omitempty"`
	Mode        string `json:"mode"`
}

func (s *Service) CreateMeeting(ctx context.Context, input CreateMeetingInput) (CreateMeetingOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return CreateMeetingOutput{}, internalError("meet connector is not configured")
	}
	mode, ok := normalizeMode(input.Mode)
	if !ok {
		return CreateMeetingOutput{}, invalidInput("mode must be one of: for_later, instant")
	}
	space, err := s.connector.CreateSpace(ctx)
	if err != nil {
		return CreateMeetingOutput{}, mapConnectorError(err)
	}
	return CreateMeetingOutput{
		SpaceName:   space.Name,
		MeetingURI:  strings.TrimSpace(space.MeetingURI),
		MeetingCode: strings.TrimSpace(space.MeetingCode),
		Mode:        mode,
	}, nil
}

type CreateMeetingTool struct {
	service *Service
}

func (t *CreateMeetingTool) Name() string { return ToolNameCreateMeeting }

func (t *CreateMeetingTool) Description() string {
	return "Create a standalone Google Meet link. Use mode=for_later for 'create a meeting for later' and mode=instant for 'start an instant meeting'. For scheduled calendar meetings, use calendar.createEvent with createConference=true instead."
}

func (t *CreateMeetingTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{ModeForLater, ModeInstant},
				"description": "for_later creates a Meet link to share later; instant creates a Meet link for immediate use. Use Calendar create/update with createConference=true for calendar events.",
			},
		},
		"required":             []string{"mode"},
		"additionalProperties": false,
	}
}

func (t *CreateMeetingTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (t *CreateMeetingTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }

func (t *CreateMeetingTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.CreateMeeting(ctx, CreateMeetingInput{
		Mode: stringArg(call.Arguments, "mode"),
	})
	if errShape != nil {
		return toToolError(call, errShape)
	}
	content := formatCreateMeetingOutput(output)
	result := tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
		ArtifactRef:    meetArtifactRef(output),
	}
	return result
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	return registry.RegisterWithEntry(&CreateMeetingTool{service: service}, tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"})
}

func normalizeMode(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ModeForLater:
		return ModeForLater, true
	case ModeInstant:
		return ModeInstant, true
	default:
		return "", false
	}
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func formatCreateMeetingOutput(output CreateMeetingOutput) string {
	return fmt.Sprintf(`{"spaceName":%q,"meetingUri":%q,"meetingCode":%q,"mode":%q}`, output.SpaceName, output.MeetingURI, output.MeetingCode, output.Mode)
}

func meetArtifactRef(output CreateMeetingOutput) *tools.ToolArtifactRef {
	if strings.TrimSpace(output.MeetingURI) == "" && strings.TrimSpace(output.SpaceName) == "" {
		return nil
	}
	return &tools.ToolArtifactRef{
		Kind:  "google.meet.space",
		Label: "Google Meet",
		ID:    strings.TrimSpace(output.SpaceName),
		URI:   strings.TrimSpace(output.MeetingURI),
		Meta: map[string]any{
			"meetingCode": output.MeetingCode,
			"mode":        output.Mode,
		},
	}
}

func toToolError(call tools.ToolCall, errShape *ErrorShape) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  fmt.Sprintf("Error: %s - %s", errShape.Code, errShape.Message),
		ContentForUser: fmt.Sprintf("Loi: %s", errShape.Message),
		Error: &tools.ToolError{
			Code:    errShape.Code,
			Message: errShape.Message,
		},
	}
}

func mapConnectorError(err error) *ErrorShape {
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: office.ErrorProviderTimeout, Message: office.FriendlyGoogleToolError(office.ErrorProviderTimeout, "Google Meet", err.Error()), Retryable: true}
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		message := googleAPIErrorMessage(gerr)
		switch {
		case gerr.Code == http.StatusUnauthorized:
			return &ErrorShape{Code: office.ErrorAuthExpired, Message: office.FriendlyGoogleToolError(office.ErrorAuthExpired, "Google Meet", message), Retryable: true}
		case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
			return &ErrorShape{Code: office.ErrorAuthMissingScope, Message: office.FriendlyGoogleToolError(office.ErrorAuthMissingScope, "Google Meet", message), Retryable: false}
		case gerr.Code == http.StatusForbidden:
			return &ErrorShape{Code: office.ErrorActionBlockedByPolicy, Message: office.FriendlyGoogleToolError(office.ErrorActionBlockedByPolicy, "Google Meet", message), Retryable: false}
		case gerr.Code == http.StatusNotFound:
			return &ErrorShape{Code: office.ErrorResourceNotFound, Message: office.FriendlyGoogleToolError(office.ErrorResourceNotFound, "Google Meet", message), Retryable: false}
		case gerr.Code == http.StatusTooManyRequests:
			return &ErrorShape{Code: office.ErrorRateLimited, Message: office.FriendlyGoogleToolError(office.ErrorRateLimited, "Google Meet", message), Retryable: true}
		case gerr.Code >= 500:
			return &ErrorShape{Code: office.ErrorProviderUnavailable, Message: office.FriendlyGoogleToolError(office.ErrorProviderUnavailable, "Google Meet", message), Retryable: true}
		default:
			return &ErrorShape{Code: "INTERNAL_ERROR", Message: message, Retryable: false}
		}
	}
	if errors.Is(err, common.ErrAuth) {
		return &ErrorShape{Code: office.ErrorAuthExpired, Message: office.FriendlyGoogleToolError(office.ErrorAuthExpired, "Google Meet", err.Error()), Retryable: true}
	}
	if errors.Is(err, common.ErrNotFound) {
		return &ErrorShape{Code: office.ErrorResourceNotFound, Message: office.FriendlyGoogleToolError(office.ErrorResourceNotFound, "Google Meet", err.Error()), Retryable: false}
	}
	if errors.Is(err, common.ErrRateLimit) {
		return &ErrorShape{Code: office.ErrorRateLimited, Message: office.FriendlyGoogleToolError(office.ErrorRateLimited, "Google Meet", err.Error()), Retryable: true}
	}
	if errors.Is(err, common.ErrAPI) {
		return &ErrorShape{Code: office.ErrorProviderUnavailable, Message: office.FriendlyGoogleToolError(office.ErrorProviderUnavailable, "Google Meet", err.Error()), Retryable: true}
	}
	return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error(), Retryable: false}
}

func googleAPIErrorMessage(err *googleapi.Error) string {
	if err == nil {
		return "Google Meet API error"
	}
	if strings.TrimSpace(err.Message) != "" {
		return err.Message
	}
	if strings.TrimSpace(err.Body) != "" {
		return err.Body
	}
	if strings.TrimSpace(err.Error()) != "" {
		return err.Error()
	}
	return fmt.Sprintf("Google Meet API error status %d", err.Code)
}

func hasMissingScopeReason(err *googleapi.Error) bool {
	text := strings.ToLower(err.Message + " " + err.Body)
	for _, item := range err.Errors {
		text += " " + strings.ToLower(item.Reason+" "+item.Message)
	}
	return strings.Contains(text, "insufficient authentication scopes") ||
		strings.Contains(text, "insufficient permissions")
}

func invalidInput(message string) *ErrorShape {
	return &ErrorShape{Code: "INVALID_INPUT", Message: message, Retryable: false}
}

func internalError(message string) *ErrorShape {
	return &ErrorShape{Code: "INTERNAL_ERROR", Message: message, Retryable: false}
}
