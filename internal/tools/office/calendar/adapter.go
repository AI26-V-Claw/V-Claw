package calendar

import (
	"context"
	"encoding/json"
	"fmt"

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

// --- CreateEvent Tool ---

// CreateEventTool implements tools.Tool for calendar.createEvent.
type CreateEventTool struct {
	service *Service
}

func (t *CreateEventTool) Name() string { return ToolNameCreateEvent }

func (t *CreateEventTool) Description() string {
	return "Create a new event in Google Calendar."
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
				"description": "Event start time in ISO-8601 format",
			},
			"end": map[string]any{
				"type":        "string",
				"description": "Event end time in ISO-8601 format",
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
		},
		"required":             []string{"title", "start", "end"},
		"additionalProperties": false,
	}
}

func (t *CreateEventTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (t *CreateEventTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t *CreateEventTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	input := CreateEventInput{
		Title:       stringArg(call.Arguments, "title"),
		Start:       stringArg(call.Arguments, "start"),
		End:         stringArg(call.Arguments, "end"),
		Attendees:   stringSliceArg(call.Arguments, "attendees"),
		Location:    stringArg(call.Arguments, "location"),
		Description: stringArg(call.Arguments, "description"),
	}

	output, errShape := t.service.CreateEvent(ctx, input)
	if errShape != nil {
		return toToolError(call, errShape)
	}

	return toToolSuccess(call, formatCreateEventOutput(output))
}

// --- UpdateEvent Tool ---

// UpdateEventTool implements tools.Tool for calendar.updateEvent.
type UpdateEventTool struct {
	service *Service
}

func (t *UpdateEventTool) Name() string { return ToolNameUpdateEvent }

func (t *UpdateEventTool) Description() string {
	return "Update an existing event in Google Calendar."
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
				"description": "Updated list of attendee email addresses",
			},
			"location": map[string]any{
				"type":        "string",
				"description": "New event location",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "New event description",
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
		EventID:     stringArg(call.Arguments, "eventId"),
		Title:       stringArg(call.Arguments, "title"),
		Start:       stringArg(call.Arguments, "start"),
		End:         stringArg(call.Arguments, "end"),
		Attendees:   stringSliceArg(call.Arguments, "attendees"),
		Location:    stringArg(call.Arguments, "location"),
		Description: stringArg(call.Arguments, "description"),
	}

	output, errShape := t.service.UpdateEvent(ctx, input)
	if errShape != nil {
		return toToolError(call, errShape)
	}

	return toToolSuccess(call, formatUpdateEventOutput(output))
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

	return toToolSuccess(call, "Event deleted successfully.")
}

// --- Registration ---

// RegisterTools registers all calendar tools with the given ToolRegistry.
func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, t := range []tools.Tool{
		&ListEventsTool{service: service},
		&CreateEventTool{service: service},
		&UpdateEventTool{service: service},
		&DeleteEventTool{service: service},
	} {
		if err := registry.RegisterWithEntry(t, tools.ToolRegistryEntry{Owner: "integration"}); err != nil {
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
		return "No events found in the specified time range."
	}
	data, err := json.Marshal(output.Events)
	if err != nil {
		return fmt.Sprintf("Found %d events.", len(output.Events))
	}
	return string(data)
}

func formatCreateEventOutput(output CreateEventOutput) string {
	data, err := json.Marshal(output.Event)
	if err != nil {
		return fmt.Sprintf("Event created with ID: %s", output.EventID)
	}
	return fmt.Sprintf("Event created: %s", string(data))
}

func formatUpdateEventOutput(output UpdateEventOutput) string {
	data, err := json.Marshal(output.Event)
	if err != nil {
		return "Event updated successfully."
	}
	return fmt.Sprintf("Event updated: %s", string(data))
}
