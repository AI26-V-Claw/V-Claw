package longmem

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"vclaw/internal/providers"
)

const memorySourcesFile = "memory_sources.json"

var memoryMarkerPattern = regexp.MustCompile(`\s*<!--\s*mem:([a-zA-Z0-9_\-]+)\s*-->`)

type FlushInput struct {
	Summary         string
	SessionID       string
	RunID           string
	RequestID       string
	ObservedAt      time.Time
	ClassifierModel string
}

type HabitInput struct {
	Transcript []providers.Message
	SessionID  string
	RunID      string
	RequestID  string
	ObservedAt time.Time
}

type memorySources struct {
	Version int                         `json:"version"`
	Facts   map[string]memoryFactSource `json:"facts"`
}

type memoryFactSource struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	File         string              `json:"file"`
	Section      string              `json:"section"`
	Text         string              `json:"text"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
	Observations []memoryObservation `json:"observations"`
}

type memoryObservation struct {
	SourceType      string    `json:"sourceType"`
	SessionID       string    `json:"sessionId,omitempty"`
	RunID           string    `json:"runId,omitempty"`
	RequestID       string    `json:"requestId,omitempty"`
	SummaryHash     string    `json:"summaryHash,omitempty"`
	ClassifierModel string    `json:"classifierModel,omitempty"`
	Count           int       `json:"count,omitempty"`
	ObservedAt      time.Time `json:"observedAt"`
}

type sourceFact struct {
	File        string
	Section     string
	Text        string
	Kind        string
	SourceType  string
	SessionID   string
	RunID       string
	RequestID   string
	SummaryHash string
	Model       string
	Count       int
	ObservedAt  time.Time
}

func appendMemoryMarker(file, section, fact string) string {
	fact = strings.TrimSpace(stripMemoryMarkers(fact))
	if fact == "" {
		return ""
	}
	return fact + " <!-- mem:" + memoryFactID(file, section, fact) + " -->"
}

func stripMemoryMarkers(text string) string {
	return strings.TrimSpace(memoryMarkerPattern.ReplaceAllString(text, ""))
}

func memoryFactID(file, section, fact string) string {
	sum := sha1.Sum([]byte(strings.Join([]string{
		strings.TrimSpace(file),
		strings.TrimSpace(section),
		normalizeFact(stripMemoryMarkers(fact)),
	}, "\n")))
	return "mem_" + hex.EncodeToString(sum[:])[:12]
}

func memorySummaryHash(summary string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(summary)))
	return hex.EncodeToString(sum[:])[:12]
}

func (f *Flusher) recordSources(facts []sourceFact) error {
	if len(facts) == 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.recordSourcesLocked(facts)
}

func (f *Flusher) recordSourcesLocked(facts []sourceFact) error {
	if len(facts) == 0 {
		return nil
	}
	store := f.readSourcesLocked()
	if store.Version == 0 {
		store.Version = 1
	}
	if store.Facts == nil {
		store.Facts = map[string]memoryFactSource{}
	}
	now := time.Now().UTC()
	for _, fact := range facts {
		text := strings.TrimSpace(stripMemoryMarkers(fact.Text))
		if text == "" {
			continue
		}
		observedAt := fact.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		id := memoryFactID(fact.File, fact.Section, text)
		entry := store.Facts[id]
		if entry.ID == "" {
			entry = memoryFactSource{
				ID:        id,
				CreatedAt: observedAt,
			}
		}
		entry.Kind = strings.TrimSpace(fact.Kind)
		entry.File = strings.TrimSpace(fact.File)
		entry.Section = strings.TrimSpace(fact.Section)
		entry.Text = text
		observation := memoryObservation{
			SourceType:      strings.TrimSpace(fact.SourceType),
			SessionID:       strings.TrimSpace(fact.SessionID),
			RunID:           strings.TrimSpace(fact.RunID),
			RequestID:       strings.TrimSpace(fact.RequestID),
			SummaryHash:     strings.TrimSpace(fact.SummaryHash),
			ClassifierModel: strings.TrimSpace(fact.Model),
			Count:           fact.Count,
			ObservedAt:      observedAt,
		}
		entry.UpdatedAt = observedAt
		if !hasMemoryObservation(entry.Observations, observation) {
			entry.Observations = append(entry.Observations, observation)
		}
		store.Facts[id] = entry
	}
	return f.writeSourcesLocked(store)
}

func hasMemoryObservation(observations []memoryObservation, candidate memoryObservation) bool {
	for _, existing := range observations {
		if existing.SourceType == candidate.SourceType &&
			existing.SessionID == candidate.SessionID &&
			existing.RunID == candidate.RunID &&
			existing.RequestID == candidate.RequestID &&
			existing.SummaryHash == candidate.SummaryHash &&
			existing.ClassifierModel == candidate.ClassifierModel &&
			existing.Count == candidate.Count &&
			existing.ObservedAt.Equal(candidate.ObservedAt) {
			return true
		}
	}
	return false
}

func (f *Flusher) readSourcesLocked() memorySources {
	data, err := os.ReadFile(filepath.Join(f.dir, memorySourcesFile))
	if err != nil {
		return memorySources{Version: 1, Facts: map[string]memoryFactSource{}}
	}
	var store memorySources
	if err := json.Unmarshal(data, &store); err != nil {
		return memorySources{Version: 1, Facts: map[string]memoryFactSource{}}
	}
	if store.Facts == nil {
		store.Facts = map[string]memoryFactSource{}
	}
	return store
}

func (f *Flusher) writeSourcesLocked(store memorySources) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(f.dir, memorySourcesFile), data)
}

func classifiedSourceFacts(result ClassifyResult, input FlushInput, summary string) []sourceFact {
	var facts []sourceFact
	hash := memorySummaryHash(summary)
	model := strings.TrimSpace(input.ClassifierModel)
	for _, fact := range result.UserFacts {
		facts = append(facts, sourceFact{
			File:        "USER.md",
			Section:     fact.Category,
			Text:        fact.Fact,
			Kind:        userFactKind(fact.Category),
			SourceType:  "session_compaction",
			SessionID:   input.SessionID,
			RunID:       input.RunID,
			RequestID:   input.RequestID,
			SummaryHash: hash,
			Model:       model,
			ObservedAt:  input.ObservedAt,
		})
	}
	facts = append(facts, notesSourceFacts(result.NotesFacts, "session_compaction", input, summary)...)
	return facts
}

func notesSourceFacts(notes []string, sourceType string, input FlushInput, summary string) []sourceFact {
	facts := make([]sourceFact, 0, len(notes))
	hash := memorySummaryHash(summary)
	model := strings.TrimSpace(input.ClassifierModel)
	for _, fact := range notes {
		facts = append(facts, sourceFact{
			File:        "NOTES.md",
			Section:     "Ghi chú phiên",
			Text:        fact,
			Kind:        notesFactKind(fact),
			SourceType:  sourceType,
			SessionID:   input.SessionID,
			RunID:       input.RunID,
			RequestID:   input.RequestID,
			SummaryHash: hash,
			Model:       model,
			ObservedAt:  input.ObservedAt,
		})
	}
	return facts
}

func userFactKind(category string) string {
	switch strings.TrimSpace(category) {
	case userCategories[1], "Thói quen làm việc":
		return "work_habit"
	case userCategories[2]:
		return "user_contact"
	case userCategories[3]:
		return "work_rule"
	case userCategories[4]:
		return "project"
	case userCategories[5]:
		return "document"
	default:
		return "user_profile"
	}
}

func notesFactKind(fact string) string {
	lower := strings.ToLower(foldVietnamese(fact))
	switch {
	case strings.Contains(lower, "du an") || strings.Contains(lower, "project"):
		return "project"
	case strings.Contains(lower, "tai lieu") || strings.Contains(lower, "document") || strings.Contains(lower, "file"):
		return "document"
	default:
		return "session_note"
	}
}
