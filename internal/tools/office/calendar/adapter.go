package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vclaw/internal/tools"
)

// --- ListEvents Tool ---

// ListEventsTool implements tools.Tool for calendar.listEvents.
type ListEventsTool struct {
	service *Service
}

func (t *ListEventsTool) Name() string { return ToolNameListEvents }

func (t *ListEventsTool) Description() string {
	return "List events from Google Calendar within a concrete time range. Convert natural ranges like today, this week, or next week into timeMin/timeMax before calling this tool."
}

func (t *ListEventsTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"timeMin": map[string]any{
				"type":        "string",
				"description": "Start of time range in ISO-8601 format. For this week, use Monday 00:00 in the user's local timezone.",
			},
			"timeMax": map[string]any{
				"type":        "string",
				"description": "Exclusive end of time range in ISO-8601 format. For this week, use next Monday 00:00 in the user's local timezone.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Optional free-text search query for event title, description, location, or attendee keywords. Do not include date/range words like today, this week, hôm nay, or tuần này.",
			},
		},
		"required":             []string{"timeMin", "timeMax"},
		"additionalProperties": false,
	}
}

func (t *ListEventsTool) Capability() tools.Capability { return tools.CapabilityReadOnly }

func (t *ListEventsTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelSafeRead }

func (t *ListEventsTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	input := ListEventsInput{
		TimeMin: stringArg(call.Arguments, "timeMin"),
		TimeMax: stringArg(call.Arguments, "timeMax"),
		Query:   stringArg(call.Arguments, "query"),
	}

	output, errShape := t.service.ListEvents(ctx, input)
	if errShape != nil {
		return toToolError(call, errShape)
	}

	return toToolSuccess(call, formatListEventsOutput(output))
}

// --- GetEvent Tool ---

// GetEventTool implements tools.Tool for calendar.getEvent.
type GetEventTool struct {
	service *Service
}

func (t *GetEventTool) Name() string { return ToolNameGetEvent }

func (t *GetEventTool) Description() string {
	return "Get details for one Google Calendar event by eventId, including organizer, creator, attendees, location, description, event link, Meet link, and recurrence flag."
}

func (t *GetEventTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"eventId": map[string]any{
				"type":        "string",
				"description": "ID of the event to inspect. Use calendar.listEvents first if the event ID is unknown.",
			},
		},
		"required":             []string{"eventId"},
		"additionalProperties": false,
	}
}

func (t *GetEventTool) Capability() tools.Capability { return tools.CapabilityReadOnly }

func (t *GetEventTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelSafeRead }

func (t *GetEventTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.GetEvent(ctx, GetEventInput{EventID: stringArg(call.Arguments, "eventId")})
	if errShape != nil {
		return toToolError(call, errShape)
	}

	result := toToolSuccess(call, formatGetEventOutput(output))
	result.ArtifactRef = calendarEventArtifactRef(output.Event)
	return result
}

// --- CreateEvent Tool ---

// CreateEventTool implements tools.Tool for calendar.createEvent.
type CreateEventTool struct {
	service *Service
}

func (t *CreateEventTool) Name() string { return ToolNameCreateEvent }

func (t *CreateEventTool) Description() string {
	return "Create a new event in Google Calendar. Title, explicit start date+time, and explicit end date+time or duration are required. A date-only phrase like tomorrow is not a start time. If start time or end time/duration is missing, ask one clarification question for all missing time fields before calling this tool. Do not assume or auto-fill a default duration. Set createConference=true when the user asks to schedule the event with Google Meet."
}

func (t *CreateEventTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Event title",
			},
			"start": map[string]any{
				"type":        "string",
				"description": "Event start time in ISO-8601 format. Must include an explicit time of day from the user; a date-only phrase like tomorrow is insufficient.",
			},
			"end": map[string]any{
				"type":        "string",
				"description": "Event end time in ISO-8601 format. Must come from an explicit user-provided end time or duration. If not provided, ask for it before calling this tool.",
			},
			"attendees": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "List of attendee email addresses",
			},
			"location": map[string]any{
				"type":        "string",
				"description": "Event location",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Event description or notes",
			},
			"createConference": map[string]any{
				"type":        "boolean",
				"description": "Set true only when the user asks for a Google Meet link for this Calendar event. Google Calendar generates the Meet link; never pass or reuse a meetLink manually.",
			},
		},
		"required":             []string{"title", "start", "end"},
		"additionalProperties": false,
	}
}

func (t *CreateEventTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (t *CreateEventTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t *CreateEventTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	input := CreateEventInput{
		Title:            stringArg(call.Arguments, "title"),
		Start:            stringArg(call.Arguments, "start"),
		End:              stringArg(call.Arguments, "end"),
		Attendees:        stringSliceArg(call.Arguments, "attendees"),
		Location:         stringArg(call.Arguments, "location"),
		Description:      stringArg(call.Arguments, "description"),
		CreateConference: boolArg(call.Arguments, "createConference"),
	}

	output, errShape := t.service.CreateEvent(ctx, input)
	if errShape != nil {
		return toToolError(call, errShape)
	}

	result := toToolSuccess(call, formatCreateEventOutput(output))
	result.ArtifactRef = calendarEventArtifactRef(output.Event)
	return result
}

// calendarEventArtifactRef returns a typed reference to a created calendar event,
// carrying the Meet link (when present) through Meta so the messenger no longer
// has to parse it back out of the result text.
func calendarEventArtifactRef(event EventSummary) *tools.ToolArtifactRef {
	if event.ID == "" {
		return nil
	}
	ref := &tools.ToolArtifactRef{
		Kind:  "calendar.event",
		Label: "Google Calendar event",
		ID:    event.ID,
		URI:   strings.TrimSpace(event.EventLink),
	}
	if event.MeetLink != "" {
		ref.Meta = map[string]any{"meetLink": event.MeetLink}
	}
	return ref
}

// --- UpdateEvent Tool ---

// UpdateEventTool implements tools.Tool for calendar.updateEvent.
type UpdateEventTool struct {
	service *Service
}

func (t *UpdateEventTool) Name() string { return ToolNameUpdateEvent }

func (t *UpdateEventTool) Description() string {
	return "Update an existing event in Google Calendar. When attendees are provided, they are added to the existing attendee list while preserving existing attendee responseStatus values. Set createConference=true when the user asks to add Google Meet to the event. Do not use this to RSVP or change an attendee responseStatus; use calendar.respondEvent for accept, decline, tentative, or needsAction."
}

func (t *UpdateEventTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"eventId": map[string]any{
				"type":        "string",
				"description": "ID of the event to update",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "New event title",
			},
			"start": map[string]any{
				"type":        "string",
				"description": "New start time in ISO-8601 format",
			},
			"end": map[string]any{
				"type":        "string",
				"description": "New end time in ISO-8601 format",
			},
			"attendees": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Attendee email addresses to add. Existing attendees are preserved, including responseStatus. Do not pass attendee objects or responseStatus here; use calendar.respondEvent for RSVP.",
			},
			"location": map[string]any{
				"type":        "string",
				"description": "New event location",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "New event description",
			},
			"createConference": map[string]any{
				"type":        "boolean",
				"description": "Set true to add a Google Meet link to the existing event. If the event already has a Meet link, the operation is idempotent. Never pass or reuse a meetLink manually.",
			},
		},
		"required":             []string{"eventId"},
		"additionalProperties": false,
	}
}

func (t *UpdateEventTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (t *UpdateEventTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t *UpdateEventTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	input := UpdateEventInput{
		EventID:          stringArg(call.Arguments, "eventId"),
		Title:            stringArg(call.Arguments, "title"),
		Start:            stringArg(call.Arguments, "start"),
		End:              stringArg(call.Arguments, "end"),
		Attendees:        stringSliceArg(call.Arguments, "attendees"),
		Location:         stringArg(call.Arguments, "location"),
		Description:      stringArg(call.Arguments, "description"),
		CreateConference: boolArg(call.Arguments, "createConference"),
	}

	output, errShape := t.service.UpdateEvent(ctx, input)
	if errShape != nil {
		return toToolError(call, errShape)
	}

	result := toToolSuccess(call, formatUpdateEventOutput(output))
	result.ArtifactRef = calendarEventArtifactRef(output.Event)
	return result
}

// --- RespondEvent Tool ---

// RespondEventTool implements tools.Tool for calendar.respondEvent.
type RespondEventTool struct {
	service *Service
}

func (t *RespondEventTool) Name() string { return ToolNameRespondEvent }

func (t *RespondEventTool) Description() string {
	return "Respond to a Google Calendar event invitation. Use this when the user asks to confirm attendance, accept, decline, mark tentative, or reset their RSVP for an event."
}

func (t *RespondEventTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"eventId": map[string]any{
				"type":        "string",
				"description": "ID of the event to respond to. Use calendar.listEvents or calendar.getEvent first if unknown.",
			},
			"eventTitle": map[string]any{
				"type":        "string",
				"description": "Optional event title for approval display. If known from calendar.listEvents or calendar.getEvent, include it so the user sees a readable event name instead of an ID.",
			},
			"email": map[string]any{
				"type":        "string",
				"description": "Attendee email to update. Optional if the event contains an attendee marked as self.",
			},
			"responseStatus": map[string]any{
				"type":        "string",
				"enum":        []string{"accepted", "declined", "tentative", "needsAction"},
				"description": "RSVP status. Use accepted to confirm attendance, declined to reject, tentative if unsure, or needsAction to reset.",
			},
		},
		"required":             []string{"eventId", "responseStatus"},
		"additionalProperties": false,
	}
}

func (t *RespondEventTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (t *RespondEventTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t *RespondEventTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.RespondEvent(ctx, RespondEventInput{
		EventID:        stringArg(call.Arguments, "eventId"),
		Email:          stringArg(call.Arguments, "email"),
		ResponseStatus: stringArg(call.Arguments, "responseStatus"),
	})
	if errShape != nil {
		return toToolError(call, errShape)
	}

	result := toToolSuccess(call, formatRespondEventOutput(output))
	result.ArtifactRef = calendarEventArtifactRef(output.Event)
	return result
}

// --- DeleteEvent Tool ---

// DeleteEventTool implements tools.Tool for calendar.deleteEvent.
type DeleteEventTool struct {
	service *Service
}

func (t *DeleteEventTool) Name() string { return ToolNameDeleteEvent }

func (t *DeleteEventTool) Description() string {
	return "Delete an event from Google Calendar."
}

func (t *DeleteEventTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"eventId": map[string]any{
				"type":        "string",
				"description": "ID of the event to delete",
			},
		},
		"required":             []string{"eventId"},
		"additionalProperties": false,
	}
}

func (t *DeleteEventTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (t *DeleteEventTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelDestructive }

func (t *DeleteEventTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	input := DeleteEventInput{
		EventID: stringArg(call.Arguments, "eventId"),
	}

	_, errShape := t.service.DeleteEvent(ctx, input)
	if errShape != nil {
		return toToolError(call, errShape)
	}

	return toToolSuccess(call, "Đã xóa sự kiện thành công.")
}

// --- Registration ---

// RegisterTools registers all calendar tools with the given ToolRegistry.
func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, t := range []tools.Tool{
		&ListEventsTool{service: service},
		&GetEventTool{service: service},
		&CreateEventTool{service: service},
		&UpdateEventTool{service: service},
		&RespondEventTool{service: service},
		&DeleteEventTool{service: service},
	} {
		if err := registry.RegisterWithEntry(t, tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

// --- Argument helpers ---

func stringArg(args map[string]any, key string) string {
	v, ok := args[key].(string)
	if !ok {
		return ""
	}
	return v
}

func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func boolArg(args map[string]any, key string) bool {
	v, ok := args[key].(bool)
	return ok && v
}

// --- ToolResult converters ---

func toToolSuccess(call tools.ToolCall, content string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

func toToolError(call tools.ToolCall, errShape *ErrorShape) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  fmt.Sprintf("Error: %s — %s", errShape.Code, errShape.Message),
		ContentForUser: fmt.Sprintf("Lỗi: %s", errShape.Message),
		Error: &tools.ToolError{
			Code:    errShape.Code,
			Message: errShape.Message,
		},
	}
}

// --- Output formatters ---

func formatListEventsOutput(output ListEventsOutput) string {
	if len(output.Events) == 0 {
		return "Không tìm thấy sự kiện nào trong khoảng thời gian đã chọn."
	}
	eventsPayload := make([]map[string]any, 0, len(output.Events))
	for _, event := range output.Events {
		eventsPayload = append(eventsPayload, calendarEventPayload(event))
	}
	data, err := json.Marshal(eventsPayload)
	if err != nil {
		return fmt.Sprintf("Tìm thấy %d sự kiện.", len(output.Events))
	}
	return string(data)
}

func formatGetEventOutput(output GetEventOutput) string {
	data, err := json.Marshal(calendarEventPayload(output.Event))
	if err != nil {
		return fmt.Sprintf("Tìm thấy sự kiện với ID: %s", output.Event.ID)
	}
	return string(data)
}

func formatCreateEventOutput(output CreateEventOutput) string {
	payload := map[string]any{
		"Event": calendarEventPayload(output.Event),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("Đã tạo sự kiện với ID: %s", output.EventID)
	}
	return fmt.Sprintf("Đã tạo sự kiện: %s", string(data))
}

func formatUpdateEventOutput(output UpdateEventOutput) string {
	data, err := json.Marshal(output.Event)
	if err != nil {
		return "Đã cập nhật sự kiện thành công."
	}
	return fmt.Sprintf("Đã cập nhật sự kiện: %s", string(data))
}

func formatRespondEventOutput(output RespondEventOutput) string {
	payload := map[string]any{
		"Event": calendarEventPayload(output.Event),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "Da cap nhat phan hoi tham du su kien."
	}
	return fmt.Sprintf("Da cap nhat phan hoi tham du: %s", string(data))
}

func calendarEventPayload(event EventSummary) map[string]any {
	payload := map[string]any{
		"id":               event.ID,
		"title":            event.Title,
		"description":      event.Description,
		"location":         event.Location,
		"start":            event.Start,
		"end":              event.End,
		"attendees":        event.Attendees,
		"organizer":        event.Organizer,
		"creator":          event.Creator,
		"eventLink":        event.EventLink,
		"meetLink":         event.MeetLink,
		"conferenceStatus": event.ConferenceStatus,
		"isRecurring":      event.IsRecurring,
	}
	return payload
}
