package knowledge

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var memoryMarkerPattern = regexp.MustCompile(`<!--\s*mem:[^>]+-->`)

func (s *Service) SyncLongTermMemory(ctx context.Context, query Query) error {
	if s == nil || s.repo == nil || strings.TrimSpace(s.memoryDir) == "" {
		return nil
	}
	userFacts, _ := readMemoryFacts(filepath.Join(s.memoryDir, "USER.md"))
	notesFacts, _ := readMemoryFacts(filepath.Join(s.memoryDir, "NOTES.md"))

	userNode, userOK := s.upsertNode(ctx, Node{
		Type:         NodeTypeUser,
		Title:        "V-Claw user",
		CanonicalKey: "local:user",
		Metadata:     map[string]any{"source": "long_term_memory"},
		Confidence:   0.7,
	})
	if userOK {
		s.upsertObservation(ctx, userNode.ID, "", ingestInput{
			SessionID:  query.SessionID,
			RunID:      query.RunID,
			RequestID:  query.RequestID,
			ToolName:   "memory.syncLongTerm",
			ObservedAt: query.Now,
		}, "long_term_memory", map[string]any{"file": "USER.md"}, "Long-term user memory")
	}

	for _, fact := range userFacts {
		s.upsertMemoryFact(ctx, query, fact, "USER.md", userNode)
	}
	for _, fact := range notesFacts {
		s.upsertMemoryFact(ctx, query, fact, "NOTES.md", userNode)
	}
	return nil
}

func (s *Service) upsertMemoryFact(ctx context.Context, query Query, fact string, file string, userNode Node) {
	fact = cleanMemoryFact(fact)
	if fact == "" {
		return
	}
	nodeType := NodeTypeNote
	if looksLikeProjectFact(fact) {
		nodeType = NodeTypeProject
	}
	node, ok := s.upsertNode(ctx, Node{
		Type:         nodeType,
		Title:        fact,
		CanonicalKey: "memory:" + file + ":" + stableKey(fact),
		Metadata: map[string]any{
			"source": "long_term_memory",
			"file":   file,
		},
		Confidence: 0.7,
	})
	if !ok {
		return
	}
	input := ingestInput{
		SessionID:  query.SessionID,
		RunID:      query.RunID,
		RequestID:  query.RequestID,
		ToolName:   "memory.syncLongTerm",
		ObservedAt: query.Now,
	}
	s.upsertObservation(ctx, node.ID, "", input, "long_term_memory", map[string]any{"file": file}, fact)
	if strings.TrimSpace(userNode.ID) != "" {
		edge, edgeOK := s.upsertEdge(ctx, Edge{
			FromNodeID: userNode.ID,
			ToNodeID:   node.ID,
			Relation:   RelationMentions,
			SourceKey:  "memory:" + file + ":" + stableKey(fact),
			Metadata: map[string]any{
				"source": "long_term_memory",
				"file":   file,
			},
			Confidence: 0.65,
		})
		if edgeOK {
			s.upsertObservation(ctx, "", edge.ID, input, "long_term_memory", map[string]any{"file": file}, fact)
		}
	}
}

func readMemoryFacts(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var facts []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if line != "" {
			facts = append(facts, line)
		}
	}
	return facts, scanner.Err()
}

func cleanMemoryFact(fact string) string {
	fact = strings.ToValidUTF8(fact, "")
	fact = memoryMarkerPattern.ReplaceAllString(fact, "")
	return strings.TrimSpace(fact)
}

func looksLikeProjectFact(fact string) bool {
	lower := strings.ToLower(fact)
	return strings.Contains(lower, "project") || strings.Contains(lower, "dự án") || strings.Contains(lower, "du an") || strings.Contains(lower, "repo")
}

func stableKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\n", " ", "\r", " ")
	value = replacer.Replace(value)
	if len([]rune(value)) > 96 {
		value = string([]rune(value)[:96])
	}
	return value
}
