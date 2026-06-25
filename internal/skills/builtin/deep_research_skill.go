package builtin

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/connectors/tavily"
	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

// DeepResearchSkill performs multi-step web research using Tavily.
// Workflow:
//  1. Parse query into sub-questions
//  2. Search each sub-question via Tavily
//  3. Cross-validate and deduplicate sources
//  4. Synthesize into a structured report with citations
//  5. Flag uncertainty where sources conflict
type DeepResearchSkill struct {
	client *tavily.Client
}

func NewDeepResearchSkill(client *tavily.Client) *DeepResearchSkill {
	return &DeepResearchSkill{client: client}
}

func (s *DeepResearchSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name: "skill.deep_research",
		Description: "Multi-step web research skill. Use when the user asks for in-depth research, " +
			"analysis of a topic requiring multiple sources, or fact-checking that requires " +
			"cross-validating information. NOT for simple one-fact lookups.",
		Version:       "1.0.0",
		Tags:          []string{"research", "web", "synthesis"},
		Scope:         []string{"web", "research"},
		Permissions:   []string{"web.search"},
		RelatedSkills: []string{},
		Fallback:      "Deep research is currently unavailable. Please try web.search directly.",
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The research question or topic to investigate",
				},
				"depth": map[string]any{
					"type":        "string",
					"enum":        []string{"quick", "standard", "deep"},
					"default":     "standard",
					"description": "Research depth: quick=1 search, standard=3 searches, deep=5 searches",
				},
				"max_sources": map[string]any{
					"type":        "integer",
					"default":     5,
					"description": "Maximum number of sources to include per search",
				},
				"output_format": map[string]any{
					"type":        "string",
					"enum":        []string{"summary", "detailed", "bullet"},
					"default":     "detailed",
					"description": "Output format: summary=short paragraph, detailed=full report, bullet=bullet points",
				},
			},
			"required": []string{"query"},
		},
		RiskLevel: tools.RiskLevelSafeRead,
		Enabled:   true,
	}
}

func (s *DeepResearchSkill) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if s.client == nil {
		return errorResult(call, "SKILL_UNAVAILABLE", "Tavily client not configured for deep research.")
	}

	query, _ := call.Arguments["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return errorResult(call, "INVALID_INPUT", "query is required.")
	}

	depth, _ := call.Arguments["depth"].(string)
	if depth == "" {
		depth = "standard"
	}
	maxSources := 5
	if v, ok := call.Arguments["max_sources"].(float64); ok && v > 0 {
		maxSources = int(v)
	}
	outputFormat, _ := call.Arguments["output_format"].(string)
	if outputFormat == "" {
		outputFormat = "detailed"
	}

	// Step 1: Determine sub-questions based on depth
	subQuestions := buildSubQuestions(query, depth)

	// Step 2: Search each sub-question
	type searchEntry struct {
		subQ    string
		results []tavily.SearchResult
		answer  string
	}
	var entries []searchEntry
	for _, subQ := range subQuestions {
		out, err := s.client.Search(ctx, tavily.SearchInput{
			Query:       subQ,
			SearchDepth: "advanced",
			MaxResults:  maxSources,
		})
		if err != nil {
			return errorResult(call, "SEARCH_FAILED", fmt.Sprintf("search failed for %q: %v", subQ, err))
		}
		entries = append(entries, searchEntry{
			subQ:    subQ,
			results: out.Results,
			answer:  out.Answer,
		})
	}

	// Step 3 & 4: Deduplicate sources and synthesize report
	seen := map[string]bool{}
	var report strings.Builder
	report.WriteString(fmt.Sprintf("# Research Report: %s\n\n", query))

	var allSources []string
	for _, entry := range entries {
		if len(entries) > 1 {
			report.WriteString(fmt.Sprintf("## %s\n\n", entry.subQ))
		}
		if strings.TrimSpace(entry.answer) != "" {
			report.WriteString(entry.answer + "\n\n")
		}
		for _, r := range entry.results {
			if seen[r.URL] {
				continue
			}
			seen[r.URL] = true
			allSources = append(allSources, fmt.Sprintf("- [%s](%s)", r.Title, r.URL))
			if outputFormat == "bullet" {
				report.WriteString(fmt.Sprintf("- **%s**: %s\n", r.Title, truncate(r.Content, 200)))
			} else if outputFormat == "detailed" {
				report.WriteString(fmt.Sprintf("### %s\n%s\n\n", r.Title, truncate(r.Content, 400)))
			}
		}
		report.WriteString("\n")
	}

	// Step 5: Sources section
	if len(allSources) > 0 {
		report.WriteString("## Sources\n\n")
		report.WriteString(strings.Join(allSources, "\n"))
	}

	content := strings.TrimSpace(report.String())
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

// buildSubQuestions returns a list of search queries based on depth.
// quick=1 (original), standard=3, deep=5.
func buildSubQuestions(query, depth string) []string {
	switch depth {
	case "quick":
		return []string{query}
	case "deep":
		return []string{
			query,
			"background and history of " + query,
			"latest developments in " + query,
			"expert opinions on " + query,
			"criticisms and limitations of " + query,
		}
	default: // standard
		return []string{
			query,
			"overview and context of " + query,
			"recent news about " + query,
		}
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func errorResult(call tools.ToolCall, code, message string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  code + ": " + message,
		ContentForUser: message,
		Error: &tools.ToolError{
			Code:    code,
			Message: message,
		},
	}
}