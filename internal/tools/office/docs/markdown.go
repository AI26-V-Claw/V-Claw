package docs

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	gdocs "vclaw/internal/connectors/google/docs"
	"vclaw/internal/tools"
)

var (
	markdownHeadingPattern = regexp.MustCompile(`^(#{1,3})\s+(.+)$`)
	markdownBulletPattern  = regexp.MustCompile(`^[-*+]\s+(.+)$`)
	markdownNumberPattern  = regexp.MustCompile(`^\d+[.)]\s+(.+)$`)
)

func (t DocsTool) appendMarkdown(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	documentID := strings.TrimSpace(stringArg(call.Arguments, "documentId"))
	markdown := stringArg(call.Arguments, "markdown")
	localPath := strings.TrimSpace(stringArg(call.Arguments, "localPath"))
	if strings.TrimSpace(markdown) != "" && localPath != "" {
		return outputToolResult(call, nil, invalidInput("provide exactly one of markdown or localPath"))
	}
	var sourceBytes int
	if localPath != "" {
		if t.guard == nil {
			return outputToolResult(call, nil, invalidInput("localPath is unavailable: no sandbox workspace is configured"))
		}
		resolved, err := t.guard.Resolve(localPath)
		if err != nil {
			return outputToolResult(call, nil, invalidInput("localPath is outside the allowed workspace: "+err.Error()))
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return outputToolResult(call, nil, invalidInput("localPath must reference a readable regular file"))
		}
		if info.Size() <= 0 || info.Size() > maxAppendLocalFileBytes {
			return outputToolResult(call, nil, invalidInput(fmt.Sprintf("localPath must contain between 1 and %d bytes", maxAppendLocalFileBytes)))
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return outputToolResult(call, nil, invalidInput("cannot read localPath: "+err.Error()))
		}
		markdown = string(data)
		sourceBytes = len(data)
	}
	if strings.TrimSpace(markdown) == "" {
		return outputToolResult(call, nil, invalidInput("provide exactly one of markdown or localPath"))
	}
	if len(markdown) > int(maxAppendLocalFileBytes) {
		return outputToolResult(call, nil, invalidInput(fmt.Sprintf("markdown exceeds the %d byte limit", maxAppendLocalFileBytes)))
	}
	if !utf8.ValidString(markdown) {
		return outputToolResult(call, nil, invalidInput("markdown must contain valid UTF-8 text"))
	}
	content := parseMarkdownRichText(markdown)
	output, errShape := t.service.AppendMarkdown(ctx, documentID, content)
	result := outputToolResult(call, output, errShape)
	if result.Success {
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["markdown_chars"] = len([]rune(markdown))
		result.Metadata["rendered_chars"] = len([]rune(content.Text))
		result.Metadata["paragraph_styles"] = len(content.ParagraphStyles)
		result.Metadata["text_styles"] = len(content.TextStyles)
		if sourceBytes > 0 {
			result.Metadata["local_file_bytes"] = sourceBytes
		}
	}
	return result
}

type markdownBuilder struct {
	text            strings.Builder
	offset          int64
	paragraphStyles []gdocs.ParagraphStyleRange
	textStyles      []gdocs.TextStyleRange
	bulletRanges    []gdocs.TextRange
	inCodeFence     bool
	codeFenceStart  int64
}

func parseMarkdownRichText(markdown string) gdocs.RichTextContent {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	b := &markdownBuilder{}
	for index := 0; index < len(lines); index++ {
		line := strings.TrimRight(lines[index], " \t\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if b.inCodeFence {
				b.textStyles = append(b.textStyles, gdocs.TextStyleRange{TextRange: gdocs.TextRange{Start: b.codeFenceStart, End: b.offset}, Monospace: true})
				b.inCodeFence = false
			} else {
				b.inCodeFence = true
				b.codeFenceStart = b.offset
			}
			continue
		}
		if b.inCodeFence {
			b.appendLine(line)
			continue
		}
		if match := markdownHeadingPattern.FindStringSubmatch(trimmed); len(match) == 3 {
			start, end := b.appendLine(cleanInlineMarkdown(match[2]))
			style := map[int]string{1: "TITLE", 2: "HEADING_1", 3: "HEADING_2"}[len(match[1])]
			b.paragraphStyles = append(b.paragraphStyles, gdocs.ParagraphStyleRange{TextRange: gdocs.TextRange{Start: start, End: end}, NamedStyleType: style})
			continue
		}
		if markdownTableLine(trimmed) {
			if index+1 < len(lines) && markdownTableSeparator(strings.TrimSpace(lines[index+1])) {
				start, end := b.appendLine(strings.Join(splitMarkdownTableRow(trimmed), "\t"))
				b.textStyles = append(b.textStyles, gdocs.TextStyleRange{TextRange: gdocs.TextRange{Start: start, End: end}, Bold: true, Monospace: true})
				index++
				continue
			}
			start, end := b.appendLine(strings.Join(splitMarkdownTableRow(trimmed), "\t"))
			b.textStyles = append(b.textStyles, gdocs.TextStyleRange{TextRange: gdocs.TextRange{Start: start, End: end}, Monospace: true})
			continue
		}
		if match := markdownBulletPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			start, end := b.appendLine(cleanInlineMarkdown(match[1]))
			b.bulletRanges = append(b.bulletRanges, gdocs.TextRange{Start: start, End: end})
			continue
		}
		if match := markdownNumberPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			start, end := b.appendLine(cleanInlineMarkdown(match[1]))
			b.bulletRanges = append(b.bulletRanges, gdocs.TextRange{Start: start, End: end})
			continue
		}
		if len(trimmed) >= 2 && strings.HasPrefix(trimmed, "_") && strings.HasSuffix(trimmed, "_") {
			start, end := b.appendLine(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "_"), "_")))
			b.textStyles = append(b.textStyles, gdocs.TextStyleRange{TextRange: gdocs.TextRange{Start: start, End: end}, Italic: true})
			continue
		}
		b.appendLine(cleanInlineMarkdown(line))
	}
	if b.inCodeFence && b.offset > b.codeFenceStart {
		b.textStyles = append(b.textStyles, gdocs.TextStyleRange{TextRange: gdocs.TextRange{Start: b.codeFenceStart, End: b.offset}, Monospace: true})
	}
	return gdocs.RichTextContent{
		Text:            b.text.String(),
		ParagraphStyles: b.paragraphStyles,
		TextStyles:      b.textStyles,
		BulletRanges:    b.bulletRanges,
	}
}

func (b *markdownBuilder) appendLine(line string) (int64, int64) {
	start := b.offset
	b.text.WriteString(line)
	b.text.WriteByte('\n')
	b.offset += utf16Length(line + "\n")
	return start, b.offset
}

func utf16Length(value string) int64 {
	return int64(len(utf16.Encode([]rune(value))))
}

func cleanInlineMarkdown(value string) string {
	value = strings.ReplaceAll(value, "**", "")
	value = strings.ReplaceAll(value, "__", "")
	value = strings.ReplaceAll(value, "`", "")
	return value
}

func markdownTableLine(value string) bool {
	return strings.HasPrefix(value, "|") && strings.HasSuffix(value, "|") && strings.Count(value, "|") >= 2
}

func markdownTableSeparator(value string) bool {
	if !markdownTableLine(value) {
		return false
	}
	for _, cell := range splitMarkdownTableRow(value) {
		cell = strings.Trim(cell, " :-")
		if cell != "" {
			return false
		}
	}
	return true
}

func splitMarkdownTableRow(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "|")
	value = strings.TrimSuffix(value, "|")
	var cells []string
	var current strings.Builder
	escaped := false
	for _, char := range value {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '|' {
			cells = append(cells, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(char)
	}
	if escaped {
		current.WriteRune('\\')
	}
	cells = append(cells, strings.TrimSpace(current.String()))
	return cells
}
