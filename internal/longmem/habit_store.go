package longmem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vclaw/internal/providers"
)

const (
	habitPatternsFile     = "habit_patterns.json"
	habitStabilityWindow  = 72 * time.Hour
	habitObservationLimit = 50
)

type habitPatternStore struct {
	Version  int                     `json:"version"`
	Patterns map[string]habitPattern `json:"patterns"`
}

type habitPattern struct {
	Key            string                    `json:"key"`
	Fact           string                    `json:"fact"`
	Category       string                    `json:"category"`
	Count          int                       `json:"count"`
	FirstSeen      time.Time                 `json:"firstSeen"`
	LastSeen       time.Time                 `json:"lastSeen"`
	Sessions       []string                  `json:"sessions"`
	Observations   []habitPatternObservation `json:"observations,omitempty"`
	Promoted       bool                      `json:"promoted"`
	PromotedAt     *time.Time                `json:"promotedAt,omitempty"`
	PromotedFactID string                    `json:"promotedFactId,omitempty"`
}

type habitPatternObservation struct {
	SessionID  string    `json:"sessionId,omitempty"`
	RunID      string    `json:"runId,omitempty"`
	RequestID  string    `json:"requestId,omitempty"`
	RawText    string    `json:"rawText,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

func latestHabitCandidate(transcript []providers.Message) (habitCandidate, string, bool) {
	for i := len(transcript) - 1; i >= 0; i-- {
		message := transcript[i]
		if message.Role != providers.MessageRoleUser {
			continue
		}
		candidate, ok := habitCandidateFromMessage(message.Content)
		return candidate, strings.TrimSpace(message.Content), ok
	}
	return habitCandidate{}, "", false
}

func (f *Flusher) recordHabitPattern(input HabitInput, candidate habitCandidate, rawText string) (*HabitFact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	store := f.readHabitPatternsLocked()
	if store.Version == 0 {
		store.Version = 1
	}
	if store.Patterns == nil {
		store.Patterns = map[string]habitPattern{}
	}

	pattern := store.Patterns[candidate.key]
	if pattern.Key == "" {
		pattern = habitPattern{
			Key:       candidate.key,
			Fact:      candidate.fact,
			Category:  userCategories[1],
			FirstSeen: input.ObservedAt,
			Sessions:  nil,
		}
	}
	pattern.Fact = candidate.fact
	pattern.Category = userCategories[1]
	if pattern.FirstSeen.IsZero() || input.ObservedAt.Before(pattern.FirstSeen) {
		pattern.FirstSeen = input.ObservedAt
	}
	if pattern.LastSeen.IsZero() || input.ObservedAt.After(pattern.LastSeen) {
		pattern.LastSeen = input.ObservedAt
	}
	observation := habitPatternObservation{
		SessionID:  strings.TrimSpace(input.SessionID),
		RunID:      strings.TrimSpace(input.RunID),
		RequestID:  strings.TrimSpace(input.RequestID),
		RawText:    truncateHabitRawText(rawText),
		ObservedAt: input.ObservedAt,
	}
	if !hasHabitObservation(pattern.Observations, observation) {
		pattern.Count++
		pattern.Sessions = appendUniqueString(pattern.Sessions, observation.SessionID)
		pattern.Observations = append(pattern.Observations, observation)
		if len(pattern.Observations) > habitObservationLimit {
			pattern.Observations = pattern.Observations[len(pattern.Observations)-habitObservationLimit:]
		}
	}

	if pattern.Promoted {
		store.Patterns[candidate.key] = pattern
		return nil, f.writeHabitPatternsLocked(store)
	}
	if !habitEligibleForPromotion(pattern) {
		store.Patterns[candidate.key] = pattern
		return nil, f.writeHabitPatternsLocked(store)
	}

	now := input.ObservedAt
	factID := memoryFactID("USER.md", pattern.Category, pattern.Fact)
	pattern.Promoted = true
	pattern.PromotedAt = &now
	pattern.PromotedFactID = factID

	userPath := filepath.Join(f.dir, "USER.md")
	existingUser := ""
	if data, err := os.ReadFile(userPath); err == nil {
		existingUser = string(data)
	}
	categorized := CategorizedFact{Category: pattern.Category, Fact: pattern.Fact}
	if err := atomicWriteFile(userPath, []byte(mergeUserFacts(existingUser, []CategorizedFact{categorized}))); err != nil {
		return nil, err
	}
	if err := f.recordSourcesLocked([]sourceFact{{
		File:       "USER.md",
		Section:    pattern.Category,
		Text:       pattern.Fact,
		Kind:       "work_habit",
		SourceType: "repeated_habit",
		SessionID:  input.SessionID,
		RunID:      input.RunID,
		RequestID:  input.RequestID,
		Count:      pattern.Count,
		ObservedAt: input.ObservedAt,
	}}); err != nil {
		return nil, err
	}

	store.Patterns[candidate.key] = pattern
	if err := f.writeHabitPatternsLocked(store); err != nil {
		return nil, err
	}
	return &HabitFact{
		CategorizedFact: categorized,
		Count:           pattern.Count,
	}, nil
}

func habitEligibleForPromotion(pattern habitPattern) bool {
	if pattern.Count < repeatedHabitThreshold {
		return false
	}
	return distinctNonEmptyStrings(pattern.Sessions) >= 2 || pattern.LastSeen.Sub(pattern.FirstSeen) >= habitStabilityWindow
}

func (f *Flusher) readHabitPatternsLocked() habitPatternStore {
	data, err := os.ReadFile(filepath.Join(f.dir, habitPatternsFile))
	if err != nil {
		return habitPatternStore{Version: 1, Patterns: map[string]habitPattern{}}
	}
	var store habitPatternStore
	if err := json.Unmarshal(data, &store); err != nil {
		return habitPatternStore{Version: 1, Patterns: map[string]habitPattern{}}
	}
	if store.Patterns == nil {
		store.Patterns = map[string]habitPattern{}
	}
	return store
}

func (f *Flusher) writeHabitPatternsLocked(store habitPatternStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(f.dir, habitPatternsFile), data)
}

func hasHabitObservation(observations []habitPatternObservation, candidate habitPatternObservation) bool {
	for _, existing := range observations {
		if candidate.RequestID != "" && existing.RequestID == candidate.RequestID {
			return true
		}
		if existing.SessionID == candidate.SessionID &&
			existing.RunID == candidate.RunID &&
			existing.RequestID == candidate.RequestID &&
			existing.RawText == candidate.RawText &&
			existing.ObservedAt.Equal(candidate.ObservedAt) {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func distinctNonEmptyStrings(values []string) int {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	return len(seen)
}

func truncateHabitRawText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const maxRunes = 160
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}
