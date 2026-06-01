package gmail

import (
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	gmailconnector "vclaw/internal/connectors/google/gmail"

	xhtml "golang.org/x/net/html"
)

const (
	displaySourcePlain        = "plain"
	displaySourceHTMLFallback = "html_fallback"
	displaySourceRawHTML      = "raw_html"
)

const (
	defaultPreviewChars = 1600
	defaultMaxURLs      = 10
)

var (
	urlPattern     = regexp.MustCompile(`https?://\S+`)
	multiSpace     = regexp.MustCompile(`[ \t]+`)
	multiBlankLine = regexp.MustCompile(`\n{3,}`)
)

var defaultTrackingURLHints = []string{
	"c.gle/",
	"utm_",
	"click",
	"tracking",
	"notifications.google.com",
}

func buildDisplay(message gmailconnector.MessageDetail, input GetEmailInput) (DisplayOutput, *ErrorShape) {
	mode := strings.ToLower(strings.TrimSpace(input.RenderMode))
	if mode == "" {
		mode = RenderModeText
	}
	if mode != RenderModeText && mode != RenderModeRawHTML {
		return DisplayOutput{}, &ErrorShape{
			Code:      "INVALID_INPUT",
			Message:   `render mode must be "text" or "raw-html"`,
			Retryable: false,
		}
	}

	config := loadRenderConfig(input.PreviewChars)
	output := DisplayOutput{
		Mode:         mode,
		PreviewChars: config.previewChars,
	}

	switch mode {
	case RenderModeRawHTML:
		output.Source = displaySourceRawHTML
		output.Text = strings.TrimSpace(message.BodyHTML)
	default:
		output.Text, output.Source = renderDisplayText(message.BodyPlain, message.BodyHTML, config)
	}

	if !input.Full {
		preview, truncated := previewText(output.Text, config.previewChars)
		output.Text = preview
		output.Truncated = truncated
	}

	return output, nil
}

type renderConfig struct {
	previewChars  int
	maxURLs       int
	trackingHints []string
}

func loadRenderConfig(previewChars int) renderConfig {
	cfg := renderConfig{
		previewChars:  defaultPreviewChars,
		maxURLs:       defaultMaxURLs,
		trackingHints: append([]string(nil), defaultTrackingURLHints...),
	}

	if override := parseEnvInt("VCLAW_GMAIL_PREVIEW_CHARS", 100, 50000); override > 0 {
		cfg.previewChars = override
	}
	if previewChars > 0 {
		cfg.previewChars = previewChars
	}

	if override := parseEnvInt("VCLAW_GMAIL_RENDER_MAX_URLS", 1, 1000); override > 0 {
		cfg.maxURLs = override
	}

	if hints := parseCSVEnv("VCLAW_GMAIL_TRACKING_HINTS"); len(hints) > 0 {
		cfg.trackingHints = hints
	}

	return cfg
}

func parseEnvInt(name string, min int, max int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if value < min || value > max {
		return 0
	}
	return value
}

func parseCSVEnv(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	items := strings.Split(raw, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.ToLower(strings.TrimSpace(item))
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func renderDisplayText(bodyPlain string, bodyHTML string, cfg renderConfig) (string, string) {
	normalizedPlain := normalizeText(bodyPlain)
	if isReadablePlain(normalizedPlain, cfg) {
		return normalizedPlain, displaySourcePlain
	}

	if strings.TrimSpace(bodyHTML) != "" {
		if fromHTML := htmlToStructuredText(bodyHTML); strings.TrimSpace(fromHTML) != "" {
			return normalizeText(fromHTML), displaySourceHTMLFallback
		}
	}

	return normalizedPlain, displaySourcePlain
}

func normalizeText(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}

	text := html.UnescapeString(input)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")

	var b strings.Builder
	for _, r := range text {
		if isInvisibleRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	text = b.String()

	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = multiSpace.ReplaceAllString(strings.TrimSpace(line), " ")
		cleaned = append(cleaned, line)
	}

	text = strings.Join(cleaned, "\n")
	text = multiBlankLine.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func isInvisibleRune(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\ufeff', '\u2060', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e':
		return true
	default:
		return false
	}
}

func isReadablePlain(text string, cfg renderConfig) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) > 0 {
		blank := 0
		short := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				blank++
				continue
			}
			if len([]rune(line)) < 16 {
				short++
			}
		}
		if float64(blank)/float64(len(lines)) > 0.45 {
			return false
		}
		if len(lines) > 24 && float64(short)/float64(len(lines)) > 0.50 {
			return false
		}
	}

	urls := urlPattern.FindAllString(trimmed, -1)
	if len(urls) >= cfg.maxURLs {
		return false
	}
	trackingCount := 0
	for _, u := range urls {
		lower := strings.ToLower(u)
		for _, hint := range cfg.trackingHints {
			if strings.Contains(lower, hint) {
				trackingCount++
				break
			}
		}
	}
	if len(urls) > 0 && float64(trackingCount)/float64(len(urls)) > 0.60 {
		return false
	}

	garbageCount := 0
	totalCount := 0
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			totalCount++
			continue
		}
		garbageCount++
		totalCount++
	}
	if totalCount > 0 && float64(garbageCount)/float64(totalCount) > 0.08 {
		return false
	}

	return true
}

func htmlToStructuredText(rawHTML string) string {
	root, err := xhtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return ""
	}

	var b strings.Builder
	renderNodeText(&b, root)
	return collapseRenderedText(b.String())
}

func renderNodeText(b *strings.Builder, node *xhtml.Node) {
	if node == nil {
		return
	}

	switch node.Type {
	case xhtml.TextNode:
		writeInlineText(b, node.Data)
		return
	case xhtml.CommentNode, xhtml.DoctypeNode:
		return
	case xhtml.ElementNode:
		tag := strings.ToLower(node.Data)
		if shouldSkipElement(tag) {
			return
		}

		switch tag {
		case "br":
			b.WriteString("\n")
			return
		case "a":
			writeAnchorText(b, node)
			return
		case "img":
			alt := strings.TrimSpace(getAttr(node, "alt"))
			if alt != "" {
				writeInlineText(b, "[Image: "+alt+"]")
			}
			return
		case "li":
			b.WriteString("\n- ")
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				renderNodeText(b, child)
			}
			return
		}

		block := isBlockTag(tag)
		if block {
			b.WriteString("\n")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderNodeText(b, child)
		}
		if block {
			b.WriteString("\n")
		}
		return
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		renderNodeText(b, child)
	}
}

func writeAnchorText(b *strings.Builder, node *xhtml.Node) {
	var text strings.Builder
	extractNodeText(&text, node)
	label := normalizeText(text.String())
	href := strings.TrimSpace(getAttr(node, "href"))
	switch {
	case label != "" && href != "":
		writeInlineText(b, label+" ("+href+")")
	case href != "":
		writeInlineText(b, href)
	case label != "":
		writeInlineText(b, label)
	}
}

func extractNodeText(b *strings.Builder, node *xhtml.Node) {
	if node == nil {
		return
	}

	if node.Type == xhtml.TextNode {
		b.WriteString(node.Data)
		return
	}

	if node.Type == xhtml.ElementNode && shouldSkipElement(strings.ToLower(node.Data)) {
		return
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		extractNodeText(b, child)
	}
}

func writeInlineText(b *strings.Builder, text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if b.Len() > 0 {
		last, _ := lastRune(b.String())
		if !unicode.IsSpace(last) && last != '\n' && last != '-' {
			b.WriteString(" ")
		}
	}
	b.WriteString(trimmed)
}

func lastRune(s string) (rune, int) {
	runes := []rune(s)
	if len(runes) == 0 {
		return 0, 0
	}
	return runes[len(runes)-1], len(runes)
}

func shouldSkipElement(tag string) bool {
	switch tag {
	case "script", "style", "head", "meta", "link", "form", "noscript", "svg", "iframe", "object", "embed":
		return true
	default:
		return false
	}
}

func isBlockTag(tag string) bool {
	switch tag {
	case "p", "div", "section", "article", "header", "footer", "main", "aside",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "table", "thead", "tbody", "tfoot", "tr", "td", "th",
		"blockquote", "pre":
		return true
	default:
		return false
	}
}

func getAttr(node *xhtml.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func collapseRenderedText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = multiSpace.ReplaceAllString(strings.TrimSpace(line), " ")
		cleaned = append(cleaned, line)
	}
	text = strings.Join(cleaned, "\n")
	text = multiBlankLine.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func previewText(text string, maxChars int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	return strings.TrimSpace(string(runes[:maxChars])) + "\n...\n[truncated]", true
}
