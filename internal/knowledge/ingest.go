package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"vclaw/internal/tools"
)

type ingestInput struct {
	RequestID  string
	SessionID  string
	RunID      string
	ToolCallID string
	ToolName   string
	Input      map[string]any
	Result     tools.ToolResult
	ObservedAt time.Time
}

func (s *Service) IngestToolResult(ctx context.Context, input ingestInput) {
	if s == nil || s.repo == nil || !input.Result.Success {
		return
	}
	switch {
	case input.ToolName == "calendar.deleteEvent":
		s.ingestCalendarDelete(ctx, input)
	case strings.HasPrefix(input.ToolName, "calendar."):
		s.ingestCalendar(ctx, input)
	case strings.HasPrefix(input.ToolName, "drive.") || strings.HasPrefix(input.ToolName, "docs."):
		s.ingestDocuments(ctx, input)
	case strings.HasPrefix(input.ToolName, "gmail."):
		s.ingestGmail(ctx, input)
	case strings.HasPrefix(input.ToolName, "chat."):
		s.ingestChat(ctx, input)
	case strings.HasPrefix(input.ToolName, "people."):
		s.ingestPeople(ctx, input)
	}
}

func (s *Service) ingestCalendarDelete(ctx context.Context, input ingestInput) {
	eventID := strings.TrimSpace(stringArg(input.Input, "eventId"))
	if eventID == "" {
		return
	}
	deletedAt := input.ObservedAt
	if deletedAt.IsZero() {
		deletedAt = time.Now().UTC()
	}
	nodeID, err := s.repo.MarkNodeDeleted(ctx, NodeRef{Type: NodeTypeMeeting, CanonicalKey: "calendar:event:" + eventID}, deletedAt)
	if err != nil {
		s.logger.Warn("knowledge calendar delete mark failed", "event_id", eventID, "error", err)
		return
	}
	if nodeID != "" {
		if err := s.repo.MarkEdgesDeletedForNode(ctx, nodeID, deletedAt); err != nil {
			s.logger.Warn("knowledge calendar delete edge mark failed", "event_id", eventID, "error", err)
		}
		s.upsertObservation(ctx, nodeID, "", input, "calendar_delete", map[string]any{"eventId": eventID}, "Calendar event deleted")
	}
}

func (s *Service) ingestCalendar(ctx context.Context, input ingestInput) {
	for _, payload := range jsonPayloads(input.Result.ContentForLLM) {
		for _, event := range calendarEventsFromPayload(payload) {
			s.ingestCalendarEvent(ctx, input, event)
		}
	}
}

type calendarEvent struct {
	ID          string
	Title       string
	Description string
	Location    string
	Start       string
	End         string
	EventLink   string
	MeetLink    string
	Organizer   personRef
	Creator     personRef
	Attendees   []personRef
	Raw         map[string]any
}

type personRef struct {
	Email       string
	DisplayName string
	Self        bool
	Status      string
}

func (s *Service) ingestCalendarEvent(ctx context.Context, input ingestInput, event calendarEvent) {
	event.ID = strings.TrimSpace(event.ID)
	if event.ID == "" {
		return
	}
	title := strings.TrimSpace(event.Title)
	if title == "" {
		title = "Calendar event"
	}
	node, ok := s.upsertNode(ctx, Node{
		Type:         NodeTypeMeeting,
		Title:        title,
		CanonicalKey: "calendar:event:" + event.ID,
		Metadata: map[string]any{
			"source":      "google_calendar",
			"eventId":     event.ID,
			"start":       event.Start,
			"end":         event.End,
			"location":    event.Location,
			"description": event.Description,
			"eventLink":   event.EventLink,
			"meetLink":    event.MeetLink,
			"toolName":    input.ToolName,
		},
		Confidence: 0.9,
	})
	if !ok {
		return
	}
	s.upsertObservation(ctx, node.ID, "", input, "google_calendar", map[string]any{"eventId": event.ID}, title)
	s.linkCalendarPerson(ctx, input, node, event.Organizer, RelationOrganizedBy, event.ID)
	s.linkCalendarPerson(ctx, input, node, event.Creator, RelationCreatedBy, event.ID)
	for _, attendee := range event.Attendees {
		s.linkCalendarPerson(ctx, input, node, attendee, RelationAttended, event.ID)
	}
}

func (s *Service) linkCalendarPerson(ctx context.Context, input ingestInput, meeting Node, person personRef, relation string, eventID string) {
	email := strings.ToLower(strings.TrimSpace(person.Email))
	if email == "" {
		return
	}
	nodeType := NodeTypePerson
	if person.Self {
		nodeType = NodeTypeUser
	}
	title := strings.TrimSpace(person.DisplayName)
	if title == "" {
		title = email
	}
	personNode, ok := s.upsertNode(ctx, Node{
		Type:         nodeType,
		Title:        title,
		CanonicalKey: "person:" + email,
		Aliases:      []string{email},
		Metadata: map[string]any{
			"email":          email,
			"displayName":    person.DisplayName,
			"responseStatus": person.Status,
			"source":         "google_calendar",
		},
		Confidence: 0.85,
	})
	if !ok {
		return
	}
	edge, ok := s.upsertEdge(ctx, Edge{
		FromNodeID: meeting.ID,
		ToNodeID:   personNode.ID,
		Relation:   relation,
		SourceKey:  fmt.Sprintf("calendar:event:%s:%s:%s", eventID, relation, email),
		Metadata: map[string]any{
			"eventId":        eventID,
			"email":          email,
			"responseStatus": person.Status,
			"source":         "google_calendar",
		},
		Confidence: 0.85,
	})
	if ok {
		s.upsertObservation(ctx, "", edge.ID, input, "google_calendar", map[string]any{"eventId": eventID, "email": email, "relation": relation}, meeting.Title)
	}
}

func (s *Service) ingestDocuments(ctx context.Context, input ingestInput) {
	if ref := input.Result.ArtifactRef; ref != nil {
		s.ingestArtifactNode(ctx, input, NodeTypeDocument, ref, 0.8)
	}
	for _, payload := range jsonPayloads(input.Result.ContentForLLM) {
		for _, item := range objectsAtKeys(payload, "File", "Files", "Document", "Documents") {
			id := firstString(item, "id", "ID", "documentId", "DocumentID", "fileId", "FileID")
			title := firstString(item, "name", "Name", "title", "Title")
			if id == "" || title == "" {
				continue
			}
			s.upsertDocumentNode(ctx, input, id, title, item)
		}
	}
}

func (s *Service) ingestGmail(ctx context.Context, input ingestInput) {
	if ref := input.Result.ArtifactRef; ref != nil {
		s.ingestArtifactNode(ctx, input, NodeTypeEmail, ref, 0.75)
	}
	for _, payload := range jsonPayloads(input.Result.ContentForLLM) {
		for _, item := range objectsAtKeys(payload, "Message", "Messages", "Email", "Emails") {
			s.ingestEmailObject(ctx, input, item)
		}
		for _, item := range objectsAtKeys(payload, "Thread", "Threads") {
			id := firstString(item, "id", "ID")
			if id == "" {
				continue
			}
			title := firstString(item, "subject", "Subject", "snippet", "Snippet")
			s.upsertEmailLikeNode(ctx, input, "gmail:thread:"+id, title, item)
		}
	}
}

func (s *Service) ingestChat(ctx context.Context, input ingestInput) {
	if ref := input.Result.ArtifactRef; ref != nil {
		s.ingestArtifactNode(ctx, input, NodeTypeChatMessage, ref, 0.75)
	}
	for _, payload := range jsonPayloads(input.Result.ContentForLLM) {
		for _, item := range objectsAtKeys(payload, "Space", "Spaces") {
			id := firstString(item, "name", "Name")
			title := firstString(item, "displayName", "DisplayName", "name", "Name")
			if id == "" {
				continue
			}
			s.upsertGenericNode(ctx, input, NodeTypeChatSpace, "chat:space:"+id, title, item, 0.75)
		}
		for _, item := range objectsAtKeys(payload, "Message", "Messages") {
			id := firstString(item, "name", "Name")
			title := firstString(item, "text", "Text", "name", "Name")
			if id == "" {
				continue
			}
			s.upsertGenericNode(ctx, input, NodeTypeChatMessage, "chat:message:"+id, title, item, 0.75)
		}
	}
}

func (s *Service) ingestPeople(ctx context.Context, input ingestInput) {
	for _, person := range peopleFromText(input.Result.ContentForLLM) {
		s.upsertPerson(ctx, input, person, "people_directory")
	}
}

func (s *Service) ingestArtifactNode(ctx context.Context, input ingestInput, nodeType string, ref *tools.ToolArtifactRef, confidence float64) {
	if ref == nil {
		return
	}
	id := strings.TrimSpace(ref.ID)
	if id == "" {
		id = strings.TrimSpace(ref.URI)
	}
	if id == "" {
		return
	}
	title := strings.TrimSpace(ref.Label)
	if title == "" {
		title = id
	}
	canonical := ref.Kind + ":" + id
	if nodeType == NodeTypeEmail && !strings.HasPrefix(canonical, "gmail:") {
		canonical = "gmail:message:" + id
	}
	node, ok := s.upsertNode(ctx, Node{
		Type:         nodeType,
		Title:        title,
		CanonicalKey: canonical,
		Metadata: map[string]any{
			"artifactKind": ref.Kind,
			"id":           id,
			"uri":          ref.URI,
			"source":       "artifact_ref",
		},
		Confidence: confidence,
	})
	if ok {
		s.upsertObservation(ctx, node.ID, "", input, "artifact_ref", map[string]any{"kind": ref.Kind, "id": id, "uri": ref.URI}, title)
	}
}

func (s *Service) upsertDocumentNode(ctx context.Context, input ingestInput, id string, title string, metadata map[string]any) {
	s.upsertGenericNode(ctx, input, NodeTypeDocument, "drive:file:"+id, title, metadata, 0.75)
}

func (s *Service) ingestEmailObject(ctx context.Context, input ingestInput, item map[string]any) {
	id := firstString(item, "id", "ID", "messageId", "MessageID")
	if id == "" {
		return
	}
	title := firstString(item, "subject", "Subject", "snippet", "Snippet")
	node, ok := s.upsertEmailLikeNode(ctx, input, "gmail:message:"+id, title, item)
	if !ok {
		return
	}
	for _, email := range splitEmails(firstString(item, "from", "From")) {
		person, personOK := s.upsertPerson(ctx, input, personRef{Email: email}, "gmail")
		if personOK {
			s.upsertEdge(ctx, Edge{FromNodeID: node.ID, ToNodeID: person.ID, Relation: RelationSentBy, SourceKey: "gmail:" + id + ":from:" + email, Metadata: map[string]any{"email": email, "source": "gmail"}, Confidence: 0.75})
		}
	}
	for _, email := range splitEmails(firstString(item, "to", "To")) {
		person, personOK := s.upsertPerson(ctx, input, personRef{Email: email}, "gmail")
		if personOK {
			s.upsertEdge(ctx, Edge{FromNodeID: node.ID, ToNodeID: person.ID, Relation: RelationSentTo, SourceKey: "gmail:" + id + ":to:" + email, Metadata: map[string]any{"email": email, "source": "gmail"}, Confidence: 0.75})
		}
	}
}

func (s *Service) upsertEmailLikeNode(ctx context.Context, input ingestInput, canonical string, title string, metadata map[string]any) (Node, bool) {
	if strings.TrimSpace(title) == "" {
		title = canonical
	}
	return s.upsertGenericNode(ctx, input, NodeTypeEmail, canonical, title, metadata, 0.75)
}

func (s *Service) upsertGenericNode(ctx context.Context, input ingestInput, nodeType string, canonical string, title string, metadata map[string]any, confidence float64) (Node, bool) {
	node, ok := s.upsertNode(ctx, Node{
		Type:         nodeType,
		Title:        title,
		CanonicalKey: canonical,
		Metadata:     normalizeMetadata(metadata),
		Confidence:   confidence,
	})
	if ok {
		s.upsertObservation(ctx, node.ID, "", input, "tool_result", map[string]any{"canonicalKey": canonical}, title)
	}
	return node, ok
}

func (s *Service) upsertPerson(ctx context.Context, input ingestInput, person personRef, source string) (Node, bool) {
	email := strings.ToLower(strings.TrimSpace(person.Email))
	displayName := strings.TrimSpace(person.DisplayName)
	key := email
	if key == "" {
		key = strings.ToLower(displayName)
	}
	if key == "" {
		return Node{}, false
	}
	title := displayName
	if title == "" {
		title = key
	}
	node, ok := s.upsertNode(ctx, Node{
		Type:         NodeTypePerson,
		Title:        title,
		CanonicalKey: "person:" + key,
		Aliases:      []string{email, displayName},
		Metadata: map[string]any{
			"email":       email,
			"displayName": displayName,
			"source":      source,
		},
		Confidence: 0.75,
	})
	if ok {
		s.upsertObservation(ctx, node.ID, "", input, source, map[string]any{"email": email, "displayName": displayName}, title)
	}
	return node, ok
}

func calendarEventsFromPayload(payload any) []calendarEvent {
	var events []calendarEvent
	switch value := payload.(type) {
	case []any:
		for _, item := range value {
			if obj, ok := asMap(item); ok {
				if event := calendarEventFromMap(obj); event.ID != "" {
					events = append(events, event)
				}
			}
		}
	case map[string]any:
		if nested, ok := value["Event"]; ok {
			if obj, ok := asMap(nested); ok {
				if event := calendarEventFromMap(obj); event.ID != "" {
					events = append(events, event)
				}
			}
			return events
		}
		if event := calendarEventFromMap(value); event.ID != "" {
			events = append(events, event)
		}
	}
	return events
}

func calendarEventFromMap(obj map[string]any) calendarEvent {
	event := calendarEvent{
		ID:          firstString(obj, "id", "ID"),
		Title:       firstString(obj, "title", "Title"),
		Description: firstString(obj, "description", "Description"),
		Location:    firstString(obj, "location", "Location"),
		Start:       firstString(obj, "start", "Start"),
		End:         firstString(obj, "end", "End"),
		EventLink:   firstString(obj, "eventLink", "EventLink"),
		MeetLink:    firstString(obj, "meetLink", "MeetLink"),
		Raw:         obj,
	}
	if organizer, ok := asMap(valueForKeys(obj, "organizer", "Organizer")); ok {
		event.Organizer = personFromMap(organizer)
	}
	if creator, ok := asMap(valueForKeys(obj, "creator", "Creator")); ok {
		event.Creator = personFromMap(creator)
	}
	if attendees, ok := valueForKeys(obj, "attendees", "Attendees").([]any); ok {
		for _, attendee := range attendees {
			if attendeeMap, ok := asMap(attendee); ok {
				event.Attendees = append(event.Attendees, personFromMap(attendeeMap))
			}
		}
	}
	return event
}

func personFromMap(obj map[string]any) personRef {
	return personRef{
		Email:       firstString(obj, "email", "Email"),
		DisplayName: firstString(obj, "displayName", "DisplayName"),
		Self:        boolValue(valueForKeys(obj, "self", "Self")),
		Status:      firstString(obj, "responseStatus", "ResponseStatus"),
	}
}

func jsonPayloads(text string) []any {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return []any{out}
	}
	start := strings.IndexAny(text, "[{")
	if start < 0 {
		return nil
	}
	candidate := text[start:]
	for len(candidate) > 0 {
		var value any
		if err := json.Unmarshal([]byte(candidate), &value); err == nil {
			return []any{value}
		}
		candidate = candidate[:len(candidate)-1]
	}
	return nil
}

func objectsAtKeys(payload any, keys ...string) []map[string]any {
	var objects []map[string]any
	if obj, ok := asMap(payload); ok {
		for _, key := range keys {
			value := valueForKeys(obj, key)
			switch typed := value.(type) {
			case []any:
				for _, item := range typed {
					if itemMap, ok := asMap(item); ok {
						objects = append(objects, itemMap)
					}
				}
			default:
				if itemMap, ok := asMap(typed); ok {
					objects = append(objects, itemMap)
				}
			}
		}
		if len(objects) == 0 {
			objects = append(objects, obj)
		}
		return objects
	}
	if list, ok := payload.([]any); ok {
		for _, item := range list {
			if itemMap, ok := asMap(item); ok {
				objects = append(objects, itemMap)
			}
		}
	}
	return objects
}

func asMap(value any) (map[string]any, bool) {
	obj, ok := value.(map[string]any)
	return obj, ok
}

func valueForKeys(obj map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			return value
		}
	}
	for existingKey, value := range obj {
		for _, key := range keys {
			if strings.EqualFold(existingKey, key) {
				return value
			}
		}
	}
	return nil
}

func firstString(obj map[string]any, keys ...string) string {
	value := valueForKeys(obj, keys...)
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}

func normalizeMetadata(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		switch typed := item.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				out[key] = typed
			}
		case float64, bool, []any, map[string]any:
			out[key] = item
		}
	}
	return out
}

func splitEmails(value string) []string {
	value = strings.ReplaceAll(value, ";", ",")
	parts := strings.Split(value, ",")
	var emails []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if start := strings.LastIndex(part, "<"); start >= 0 {
			if end := strings.LastIndex(part, ">"); end > start {
				part = part[start+1 : end]
			}
		}
		if strings.Contains(part, "@") {
			emails = append(emails, strings.ToLower(part))
		}
	}
	return emails
}

var peopleLinePattern = regexp.MustCompile(`^- ([^|]+)\|([^|]+)\| emails: ([^|]+)`)

func peopleFromText(text string) []personRef {
	var people []personRef
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		match := peopleLinePattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		displayName := strings.TrimSpace(match[2])
		emailText := strings.TrimSpace(match[3])
		for _, email := range splitEmails(emailText) {
			people = append(people, personRef{Email: email, DisplayName: displayName})
		}
	}
	return people
}
