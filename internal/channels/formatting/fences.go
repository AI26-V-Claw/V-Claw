package formatting

import "strings"

// ParseFencedCodeBlockOpen detects Markdown fenced code openings like ```python.
func ParseFencedCodeBlockOpen(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
	if rest == "" {
		return "", true
	}
	if strings.ContainsAny(rest, " \t`") {
		return "", false
	}
	return rest, true
}

func IsFencedCodeBlockClose(line string) bool {
	return strings.TrimSpace(line) == "```"
}

func ReplaceFencedCodeBlocks(text string, replace func(language, code string) string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.Contains(normalized, "```") {
		return normalized
	}

	lines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(lines))
	blockLines := make([]string, 0, 8)
	inFence := false
	language := ""

	for _, line := range lines {
		if !inFence {
			if lang, ok := ParseFencedCodeBlockOpen(line); ok {
				inFence = true
				language = lang
				blockLines = blockLines[:0]
				continue
			}
			out = append(out, line)
			continue
		}

		if IsFencedCodeBlockClose(line) {
			out = append(out, replace(language, strings.Join(blockLines, "\n")))
			inFence = false
			language = ""
			blockLines = blockLines[:0]
			continue
		}
		blockLines = append(blockLines, line)
	}

	if inFence {
		opening := "```"
		if language != "" {
			opening += language
		}
		out = append(out, opening)
		out = append(out, blockLines...)
	}

	return strings.Join(out, "\n")
}
