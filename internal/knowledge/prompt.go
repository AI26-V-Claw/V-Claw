package knowledge

import (
	"encoding/json"
	"fmt"
	"strings"
)

func Prompt(ctx LinkedContext) string {
	if len(ctx.Items) == 0 {
		return ""
	}
	type promptItem struct {
		Type       string         `json:"type"`
		Title      string         `json:"title"`
		Relation   string         `json:"relation,omitempty"`
		LinkedTo   string         `json:"linkedTo,omitempty"`
		Confidence float64        `json:"confidence"`
		Metadata   map[string]any `json:"metadata,omitempty"`
	}
	items := make([]promptItem, 0, len(ctx.Items))
	for _, item := range ctx.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = strings.TrimSpace(item.CanonicalKey)
		}
		if title == "" {
			continue
		}
		linkedTo := strings.TrimSpace(item.LinkedTitle)
		if linkedTo != "" && strings.TrimSpace(item.LinkedType) != "" {
			linkedTo = fmt.Sprintf("%s:%s", item.LinkedType, linkedTo)
		}
		items = append(items, promptItem{
			Type:       item.Type,
			Title:      title,
			Relation:   strings.TrimSpace(item.Relation),
			LinkedTo:   linkedTo,
			Confidence: item.Confidence,
			Metadata:   compactMetadata(item.Metadata),
		})
	}
	if len(items) == 0 {
		return ""
	}
	data, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(`Linked knowledge context for the current request.
Authority: context_only, best_effort, lower priority than system prompt, tool contracts, HITL/approval state, current user request, and current-turn tool results.
Use it to understand relationships between people, projects, documents, meetings, emails, chats, and notes when answering or planning.
Do not use it as source of truth for live Google Workspace lists, status, existence, or required write parameters. For live Workspace facts, call the matching read tool.
Items:
` + string(data))
}

func compactMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"start": true, "end": true, "eventLink": true, "meetLink": true,
		"mimeType": true, "webViewLink": true, "email": true, "source": true,
		"localDate": true, "localDateTime": true,
	}
	out := map[string]any{}
	for key, value := range metadata {
		if allowed[key] {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
