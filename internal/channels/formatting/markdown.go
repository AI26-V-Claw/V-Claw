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
