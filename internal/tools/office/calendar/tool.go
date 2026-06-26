package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	googleconnector "vclaw/internal/connectors/google"
	gcal "vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/common"
	"vclaw/internal/tools/office"

	"google.golang.org/api/googleapi"
)

// Tool names following contract naming convention: <domain>.<action>
const (
	ToolNameListEvents   = "calendar.listEvents"
	ToolNameGetEvent     = "calendar.getEvent"
	ToolNameCreateEvent  = "calendar.createEvent"
	ToolNameUpdateEvent  = "calendar.updateEvent"
	ToolNameRespondEvent = "calendar.respondEvent"
	ToolNameDeleteEvent  = "calendar.deleteEvent"
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
		Description:      "List events from Google Calendar within a time range. timeMin and timeMax must be RFC3339 timestamps. Use query only for event title, location, or attendee keywords — do not put date phrases like \"today\" or \"hôm nay\" in query.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameGetEvent,
		Owner:            "integration",
		Description:      "Get details for one Google Calendar event by eventId, including organizer, creator, attendees, links, location, description, and recurrence flag.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameCreateEvent,
		Owner:            "integration",
		Description:      "Create a new event in Google Calendar. Attendees must be valid email addresses — resolve person names with people.searchDirectory before calling this tool.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameUpdateEvent,
		Owner:            "integration",
		Description:      "Update an existing event in Google Calendar. Attendees must be valid email addresses — resolve person names with people.searchDirectory before calling this tool. Provided attendees are added to the existing attendee list while preserving existing responseStatus values.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameRespondEvent,
		Owner:            "integration",
		Description:      "Respond to a Google Calendar event invitation by setting an attendee responseStatus. Use accepted to confirm attendance, declined to reject, tentative if unsure, or needsAction to reset.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameDeleteEvent,
		Owner:            "integration",
		Description:      "Delete an event from Google Calendar. After a bulk delete batch, call calendar.listEvents with the same time range to verify the range is empty; repeat if events remain.",
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
	RespondEvent(ctx context.Context, eventID string, email string, responseStatus string) (gcal.Event, error)
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
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	Location         string         `json:"location,omitempty"`
	Start            string         `json:"start"`
	End              string         `json:"end"`
	Attendees        []AttendeeInfo `json:"attendees,omitempty"`
	Organizer        PersonInfo     `json:"organizer,omitempty"`
	Creator          PersonInfo     `json:"creator,omitempty"`
	EventLink        string         `json:"eventLink,omitempty"`
	MeetLink         string         `json:"meetLink,omitempty"`
	ConferenceStatus string         `json:"conferenceStatus,omitempty"`
	IsRecurring      bool           `json:"isRecurring,omitempty"`
}

// AttendeeInfo represents a participant in a calendar event.
type AttendeeInfo struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
}

// PersonInfo represents an event organizer or creator.
type PersonInfo struct {
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Self        bool   `json:"self,omitempty"`
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

// GetEventInput is the input for calendar.getEvent.
type GetEventInput struct {
	EventID string // required
}

// GetEventOutput is the output for calendar.getEvent.
type GetEventOutput struct {
	Event EventSummary
}

// CreateEventInput is the input for calendar.createEvent.
type CreateEventInput struct {
	Title            string   // required
	Start            string   // ISO-8601, required
	End              string   // ISO-8601, required
	Attendees        []string // email addresses, optional
	Location         string   // optional
	Description      string   // optional
	CreateConference bool     // optional; asks Calendar to generate a Google Meet link
}

// CreateEventOutput is the output for calendar.createEvent.
type CreateEventOutput struct {
	EventID string
	Event   EventSummary
}

// UpdateEventInput is the input for calendar.updateEvent.
type UpdateEventInput struct {
	EventID          string   // required
	Title            string   // optional
	Start            string   // optional, ISO-8601
	End              string   // optional, ISO-8601
	Attendees        []string // optional
	Location         string   // optional
	Description      string   // optional
	CreateConference bool     // optional; asks Calendar to generate a Google Meet link
}

// UpdateEventOutput is the output for calendar.updateEvent.
type UpdateEventOutput struct {
	Event EventSummary
}

// RespondEventInput is the input for calendar.respondEvent.
type RespondEventInput struct {
	EventID        string // required
	Email          string // optional; if empty, use the attendee marked self
	ResponseStatus string // required: accepted, declined, tentative, needsAction
}

// RespondEventOutput is the output for calendar.respondEvent.
type RespondEventOutput struct {
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

// GetEvent retrieves one event by ID.
func (s *Service) GetEvent(ctx context.Context, input GetEventInput) (GetEventOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return GetEventOutput{}, internalError("calendar connector is not configured")
	}

	if strings.TrimSpace(input.EventID) == "" {
		return GetEventOutput{}, invalidInput("eventId is required")
	}

	event, err := s.connector.GetEvent(ctx, input.EventID)
	if err != nil {
		return GetEventOutput{}, mapConnectorError(err)
	}

	return GetEventOutput{Event: toEventSummary(event)}, nil
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
		Title:            input.Title,
		Description:      input.Description,
		Location:         input.Location,
		StartTime:        startTime,
		EndTime:          endTime,
		CreateConference: input.CreateConference,
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
		Title:            input.Title,
		Description:      input.Description,
		Location:         input.Location,
		CreateConference: input.CreateConference,
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

	var current gcal.Event
	if len(input.Attendees) > 0 || input.CreateConference {
		loaded, err := s.connector.GetEvent(ctx, input.EventID)
		if err != nil {
			return UpdateEventOutput{}, mapConnectorError(err)
		}
		current = loaded
	}

	if len(input.Attendees) > 0 {
		merged, errShape := mergeAttendeesPreservingState(current.Attendees, input.Attendees)
		if errShape != nil {
			return UpdateEventOutput{}, errShape
		}
		event.Attendees = merged
	}
	if input.CreateConference && strings.TrimSpace(current.MeetLink) != "" {
		event.CreateConference = false
		if !updateHasPatchFields(input) {
			return UpdateEventOutput{Event: toEventSummary(current)}, nil
		}
	}

	updated, err := s.connector.UpdateEvent(ctx, input.EventID, event)
	if err != nil {
		return UpdateEventOutput{}, mapConnectorError(err)
	}

	return UpdateEventOutput{Event: toEventSummary(updated)}, nil
}

func mergeAttendeesPreservingState(existing []gcal.Attendee, additions []string) ([]gcal.Attendee, *ErrorShape) {
	merged := append([]gcal.Attendee(nil), existing...)
	seen := make(map[string]struct{}, len(merged))
	for _, attendee := range merged {
		email := strings.ToLower(strings.TrimSpace(attendee.Email))
		if email != "" {
			seen[email] = struct{}{}
		}
	}

	for _, rawEmail := range additions {
		rawEmail = strings.TrimSpace(rawEmail)
		if rawEmail == "" {
			continue
		}
		parsed, err := mail.ParseAddress(rawEmail)
		if err != nil {
			return nil, invalidInput("attendee must be a valid email address: " + rawEmail)
		}
		email := strings.TrimSpace(parsed.Address)
		key := strings.ToLower(email)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, gcal.Attendee{Email: email})
	}

	return merged, nil
}

func updateHasPatchFields(input UpdateEventInput) bool {
	return strings.TrimSpace(input.Title) != "" ||
		strings.TrimSpace(input.Start) != "" ||
		strings.TrimSpace(input.End) != "" ||
		len(input.Attendees) > 0 ||
		strings.TrimSpace(input.Location) != "" ||
		strings.TrimSpace(input.Description) != ""
}

// RespondEvent updates one attendee's response status for an event.
func (s *Service) RespondEvent(ctx context.Context, input RespondEventInput) (RespondEventOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return RespondEventOutput{}, internalError("calendar connector is not configured")
	}

	if strings.TrimSpace(input.EventID) == "" {
		return RespondEventOutput{}, invalidInput("eventId is required")
	}
	email := strings.TrimSpace(input.Email)
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			return RespondEventOutput{}, invalidInput("email must be a valid email address: " + email)
		}
	}
	status, ok := normalizeResponseStatus(input.ResponseStatus)
	if !ok {
		return RespondEventOutput{}, invalidInput("responseStatus must be one of: accepted, declined, tentative, needsAction")
	}

	updated, err := s.connector.RespondEvent(ctx, input.EventID, email, status)
	if err != nil {
		return RespondEventOutput{}, mapConnectorError(err)
	}

	return RespondEventOutput{Event: toEventSummary(updated)}, nil
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
			DisplayName:    a.DisplayName,
			ResponseStatus: a.ResponseStatus,
		})
	}

	summary := EventSummary{
		ID:               e.ID,
		Title:            e.Title,
		Description:      e.Description,
		Location:         e.Location,
		Attendees:        attendees,
		Organizer:        toPersonInfo(e.Organizer),
		Creator:          toPersonInfo(e.Creator),
		EventLink:        e.EventLink,
		MeetLink:         e.MeetLink,
		ConferenceStatus: e.ConferenceStatus,
		IsRecurring:      e.IsRecurring,
	}

	if !e.StartTime.IsZero() {
		summary.Start = e.StartTime.Format(time.RFC3339)
	}
	if !e.EndTime.IsZero() {
		summary.End = e.EndTime.Format(time.RFC3339)
	}

	return summary
}

func toPersonInfo(p gcal.Person) PersonInfo {
	return PersonInfo{Email: p.Email, DisplayName: p.DisplayName, Self: p.Self}
}

func normalizeResponseStatus(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "accepted", "accept":
		return "accepted", true
	case "declined", "decline":
		return "declined", true
	case "tentative":
		return "tentative", true
	case "needsaction", "needs_action", "needs-action", "needs action":
		return "needsAction", true
	default:
		return "", false
	}
}

// mapConnectorError maps connector sentinel errors to contract ErrorShape.
func mapConnectorError(err error) *ErrorShape {
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting Calendar API: " + err.Error(), Retryable: true}
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		message := googleAPIErrorMessage(gerr)
		switch {
		case gerr.Code == http.StatusUnauthorized:
			return &ErrorShape{Code: office.ErrorAuthExpired, Message: office.FriendlyGoogleToolError(office.ErrorAuthExpired, "Google Calendar", message), Retryable: true}
		case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
			return &ErrorShape{Code: office.ErrorAuthMissingScope, Message: office.FriendlyGoogleToolError(office.ErrorAuthMissingScope, "Google Calendar", message), Retryable: false}
		case gerr.Code == http.StatusForbidden:
			return &ErrorShape{Code: office.ErrorActionBlockedByPolicy, Message: office.FriendlyGoogleToolError(office.ErrorActionBlockedByPolicy, "Google Calendar", message), Retryable: false}
		case gerr.Code == http.StatusNotFound:
			return &ErrorShape{Code: office.ErrorResourceNotFound, Message: office.FriendlyGoogleToolError(office.ErrorResourceNotFound, "Google Calendar", message), Retryable: false}
		case gerr.Code == http.StatusTooManyRequests:
			return &ErrorShape{Code: office.ErrorRateLimited, Message: office.FriendlyGoogleToolError(office.ErrorRateLimited, "Google Calendar", message), Retryable: true}
		case gerr.Code >= 500:
			return &ErrorShape{Code: office.ErrorProviderUnavailable, Message: office.FriendlyGoogleToolError(office.ErrorProviderUnavailable, "Google Calendar", message), Retryable: true}
		default:
			return &ErrorShape{Code: "INTERNAL_ERROR", Message: message, Retryable: false}
		}
	}
	if errors.Is(err, common.ErrAuth) {
		return &ErrorShape{
			Code:      office.ErrorAuthExpired,
			Message:   office.FriendlyGoogleToolError(office.ErrorAuthExpired, "Google Calendar", err.Error()),
			Retryable: true,
		}
	}
	if errors.Is(err, common.ErrNotFound) {
		return &ErrorShape{
			Code:      office.ErrorResourceNotFound,
			Message:   office.FriendlyGoogleToolError(office.ErrorResourceNotFound, "Google Calendar", err.Error()),
			Retryable: false,
		}
	}
	if errors.Is(err, common.ErrRateLimit) {
		return &ErrorShape{
			Code:      office.ErrorRateLimited,
			Message:   office.FriendlyGoogleToolError(office.ErrorRateLimited, "Google Calendar", err.Error()),
			Retryable: true,
		}
	}
	if errors.Is(err, common.ErrAPI) {
		return &ErrorShape{
			Code:      office.ErrorProviderUnavailable,
			Message:   office.FriendlyGoogleToolError(office.ErrorProviderUnavailable, "Google Calendar", err.Error()),
			Retryable: true,
		}
	}
	return &ErrorShape{
		Code:      "INTERNAL_ERROR",
		Message:   err.Error(),
		Retryable: false,
	}
}

func googleAPIErrorMessage(err *googleapi.Error) string {
	if err == nil {
		return "Google Calendar API error"
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
	return fmt.Sprintf("Google Calendar API error status %d", err.Code)
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
