package formatting

import "strings"

func ParseMarkdownHeading(line string) (level int, title string, ok bool) {
	trimmedLeft := strings.TrimLeft(line, " \t")
	if trimmedLeft == "" || trimmedLeft[0] != '#' {
		return 0, "", false
	}

	level = 0
	for level < len(trimmedLeft) && trimmedLeft[level] == '#' && level < 6 {
		level++
	}
	if level == 0 || level >= len(trimmedLeft) || trimmedLeft[level] != ' ' {
		return 0, "", false
	}

	title = strings.TrimSpace(trimmedLeft[level+1:])
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func ParseMarkdownDashListItem(line string) (level int, content string, ok bool) {
	leadingSpaces := 0
	for _, r := range line {
		if r == ' ' {
			leadingSpaces++
			continue
		}
		if r == '\t' {
			leadingSpaces += 2
			continue
		}
		break
	}

	trimmedLeft := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmedLeft, "- ") {
		return 0, "", false
	}

	content = strings.TrimSpace(trimmedLeft[2:])
	if content == "" {
		return 0, "", false
	}
	if leadingSpaces == 0 {
		return 0, content, true
	}
	return 1 + (leadingSpaces-1)/2, content, true
}
