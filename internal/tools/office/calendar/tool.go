package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	gcal "vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/common"
)

// Tool names following contract naming convention: <domain>.<action>
const (
	ToolNameListEvents  = "calendar.listEvents"
	ToolNameCreateEvent = "calendar.createEvent"
	ToolNameUpdateEvent = "calendar.updateEvent"
	ToolNameDeleteEvent = "calendar.deleteEvent"
)

// ToolRegistryEntry describes a calendar tool for documentation and policy use.
type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

// RegistryEntries lists all calendar tools per contract Section 4.
var RegistryEntries = []ToolRegistryEntry{
	{
		Name:             ToolNameListEvents,
		Owner:            "integration",
		Description:      "List events from Google Calendar within a time range.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameCreateEvent,
		Owner:            "integration",
		Description:      "Create a new event in Google Calendar.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameUpdateEvent,
		Owner:            "integration",
		Description:      "Update an existing event in Google Calendar.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameDeleteEvent,
		Owner:            "integration",
		Description:      "Delete an event from Google Calendar.",
		DefaultRiskLevel: "destructive",
		RequiresApproval: true,
	},
}

// ErrorShape represents a standardized error per contract Section 3.8.
type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ErrorShape) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Connector defines the interface for the Google Calendar API connector.
type Connector interface {
	ListEvents(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error)
	GetEvent(ctx context.Context, eventID string) (gcal.Event, error)
	CreateEvent(ctx context.Context, e gcal.Event) (gcal.Event, error)
	UpdateEvent(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error)
	DeleteEvent(ctx context.Context, eventID string) error
}

// Service implements calendar tool business logic with contract-compliant I/O.
type Service struct {
	connector Connector
}

// NewService creates a new Calendar tool Service.
func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

// --- Shared types ---

// EventSummary is the agent-facing event representation.
type EventSummary struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Location    string         `json:"location,omitempty"`
	Start       string         `json:"start"`
	End         string         `json:"end"`
	Attendees   []AttendeeInfo `json:"attendees,omitempty"`
	MeetLink    string         `json:"meetLink,omitempty"`
	IsRecurring bool           `json:"isRecurring,omitempty"`
}

// AttendeeInfo represents a participant in a calendar event.
type AttendeeInfo struct {
	Email          string `json:"email"`
	ResponseStatus string `json:"responseStatus,omitempty"`
}

// --- Input/Output types ---

// ListEventsInput is the input for calendar.listEvents.
type ListEventsInput struct {
	TimeMin string // ISO-8601, required
	TimeMax string // ISO-8601, required
	Query   string // optional free-text search
}

// ListEventsOutput is the output for calendar.listEvents.
type ListEventsOutput struct {
	Events []EventSummary
}

// CreateEventInput is the input for calendar.createEvent.
type CreateEventInput struct {
	Title       string   // required
	Start       string   // ISO-8601, required
	End         string   // ISO-8601, required
	Attendees   []string // email addresses, optional
	Location    string   // optional
	Description string   // optional
}

// CreateEventOutput is the output for calendar.createEvent.
type CreateEventOutput struct {
	EventID string
	Event   EventSummary
}

// UpdateEventInput is the input for calendar.updateEvent.
type UpdateEventInput struct {
	EventID     string   // required
	Title       string   // optional
	Start       string   // optional, ISO-8601
	End         string   // optional, ISO-8601
	Attendees   []string // optional
	Location    string   // optional
	Description string   // optional
}

// UpdateEventOutput is the output for calendar.updateEvent.
type UpdateEventOutput struct {
	Event EventSummary
}

// DeleteEventInput is the input for calendar.deleteEvent.
type DeleteEventInput struct {
	EventID string // required
}

// DeleteEventOutput is the output for calendar.deleteEvent.
type DeleteEventOutput struct{}

// --- Service methods ---

// ListEvents retrieves events within a time range.
func (s *Service) ListEvents(ctx context.Context, input ListEventsInput) (ListEventsOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListEventsOutput{}, internalError("calendar connector is not configured")
	}

	timeMin, err := time.Parse(time.RFC3339, input.TimeMin)
	if err != nil {
		return ListEventsOutput{}, invalidInput("timeMin must be in ISO-8601 format (e.g. 2026-05-29T09:00:00+07:00)")
	}

	timeMax, err := time.Parse(time.RFC3339, input.TimeMax)
	if err != nil {
		return ListEventsOutput{}, invalidInput("timeMax must be in ISO-8601 format (e.g. 2026-05-29T09:00:00+07:00)")
	}

	events, err := s.connector.ListEvents(ctx, timeMin, timeMax, normalizeListEventsQuery(input.Query))
	if err != nil {
		return ListEventsOutput{}, mapConnectorError(err)
	}

	summaries := make([]EventSummary, 0, len(events))
	for _, e := range events {
		summaries = append(summaries, toEventSummary(e))
	}

	return ListEventsOutput{Events: summaries}, nil
}

// CreateEvent creates a new calendar event.
func (s *Service) CreateEvent(ctx context.Context, input CreateEventInput) (CreateEventOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return CreateEventOutput{}, internalError("calendar connector is not configured")
	}

	if strings.TrimSpace(input.Title) == "" {
		return CreateEventOutput{}, invalidInput("title is required")
	}

	startTime, err := time.Parse(time.RFC3339, input.Start)
	if err != nil {
		return CreateEventOutput{}, invalidInput("start must be in ISO-8601 format")
	}

	endTime, err := time.Parse(time.RFC3339, input.End)
	if err != nil {
		return CreateEventOutput{}, invalidInput("end must be in ISO-8601 format")
	}

	event := gcal.Event{
		Title:       input.Title,
		Description: input.Description,
		Location:    input.Location,
		StartTime:   startTime,
		EndTime:     endTime,
	}

	for _, email := range input.Attendees {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		if _, err := mail.ParseAddress(email); err != nil {
			return CreateEventOutput{}, invalidInput("attendee must be a valid email address: " + email)
		}
		event.Attendees = append(event.Attendees, gcal.Attendee{Email: email})
	}

	created, err := s.connector.CreateEvent(ctx, event)
	if err != nil {
		return CreateEventOutput{}, mapConnectorError(err)
	}

	return CreateEventOutput{
		EventID: created.ID,
		Event:   toEventSummary(created),
	}, nil
}

// UpdateEvent updates an existing calendar event using PATCH semantics.
func (s *Service) UpdateEvent(ctx context.Context, input UpdateEventInput) (UpdateEventOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return UpdateEventOutput{}, internalError("calendar connector is not configured")
	}

	if strings.TrimSpace(input.EventID) == "" {
		return UpdateEventOutput{}, invalidInput("eventId is required")
	}

	event := gcal.Event{
		Title:       input.Title,
		Description: input.Description,
		Location:    input.Location,
	}

	if input.Start != "" {
		startTime, err := time.Parse(time.RFC3339, input.Start)
		if err != nil {
			return UpdateEventOutput{}, invalidInput("start must be in ISO-8601 format")
		}
		event.StartTime = startTime
	}

	if input.End != "" {
		endTime, err := time.Parse(time.RFC3339, input.End)
		if err != nil {
			return UpdateEventOutput{}, invalidInput("end must be in ISO-8601 format")
		}
		event.EndTime = endTime
	}

	for _, email := range input.Attendees {
		event.Attendees = append(event.Attendees, gcal.Attendee{Email: email})
	}

	updated, err := s.connector.UpdateEvent(ctx, input.EventID, event)
	if err != nil {
		return UpdateEventOutput{}, mapConnectorError(err)
	}

	return UpdateEventOutput{Event: toEventSummary(updated)}, nil
}

// DeleteEvent deletes a calendar event.
func (s *Service) DeleteEvent(ctx context.Context, input DeleteEventInput) (DeleteEventOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return DeleteEventOutput{}, internalError("calendar connector is not configured")
	}

	if strings.TrimSpace(input.EventID) == "" {
		return DeleteEventOutput{}, invalidInput("eventId is required")
	}

	err := s.connector.DeleteEvent(ctx, input.EventID)
	if err != nil {
		return DeleteEventOutput{}, mapConnectorError(err)
	}

	return DeleteEventOutput{}, nil
}

// --- Helpers ---

// toEventSummary converts a connector Event to an agent-facing EventSummary.
func toEventSummary(e gcal.Event) EventSummary {
	attendees := make([]AttendeeInfo, 0, len(e.Attendees))
	for _, a := range e.Attendees {
		attendees = append(attendees, AttendeeInfo{
			Email:          a.Email,
			ResponseStatus: a.ResponseStatus,
		})
	}

	summary := EventSummary{
		ID:          e.ID,
		Title:       e.Title,
		Description: e.Description,
		Location:    e.Location,
		Attendees:   attendees,
		MeetLink:    e.MeetLink,
		IsRecurring: e.IsRecurring,
	}

	if !e.StartTime.IsZero() {
		summary.Start = e.StartTime.Format(time.RFC3339)
	}
	if !e.EndTime.IsZero() {
		summary.End = e.EndTime.Format(time.RFC3339)
	}

	return summary
}

// mapConnectorError maps connector sentinel errors to contract ErrorShape.
func mapConnectorError(err error) *ErrorShape {
	if errors.Is(err, common.ErrAuth) {
		return &ErrorShape{
			Code:      "AUTH_EXPIRED",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	if errors.Is(err, common.ErrNotFound) {
		return &ErrorShape{
			Code:      "RESOURCE_NOT_FOUND",
			Message:   err.Error(),
			Retryable: false,
		}
	}
	if errors.Is(err, common.ErrRateLimit) {
		return &ErrorShape{
			Code:      "RATE_LIMITED",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	if errors.Is(err, common.ErrAPI) {
		return &ErrorShape{
			Code:      "PROVIDER_UNAVAILABLE",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	return &ErrorShape{
		Code:      "INTERNAL_ERROR",
		Message:   err.Error(),
		Retryable: false,
	}
}

func invalidInput(message string) *ErrorShape {
	return &ErrorShape{
		Code:      "INVALID_INPUT",
		Message:   message,
		Retryable: false,
	}
}

func internalError(message string) *ErrorShape {
	return &ErrorShape{
		Code:      "INTERNAL_ERROR",
		Message:   message,
		Retryable: false,
	}
}

func normalizeListEventsQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	dateOnlyPhrases := []string{
		"hôm nay",
		"hom nay",
		"ngày hôm nay",
		"ngay hom nay",
		"tuần này",
		"tuan nay",
		"trong tuần này",
		"trong tuan nay",
		"tuần sau",
		"tuan sau",
		"next week",
		"this week",
		"today",
		"tomorrow",
	}
	for _, phrase := range dateOnlyPhrases {
		if lower == phrase {
			return ""
		}
	}
	if containsCalendarRequestWords(lower) && containsDatePhrase(lower) && !containsLikelySearchKeyword(lower) {
		return ""
	}
	return trimmed
}

func containsCalendarRequestWords(text string) bool {
	for _, word := range []string{"lịch", "lich", "calendar", "event", "sự kiện", "su kien", "có gì", "co gi"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func containsDatePhrase(text string) bool {
	for _, phrase := range []string{"hôm nay", "hom nay", "tuần này", "tuan nay", "tuần sau", "tuan sau", "today", "this week", "next week"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func containsLikelySearchKeyword(text string) bool {
	for _, marker := range []string{"với ", "voi ", "cùng ", "cung ", "bao", "tung", "abc", "standup", "meeting", "project"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
