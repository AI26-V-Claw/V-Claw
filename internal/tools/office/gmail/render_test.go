package gmail

import (
	"strings"
	"testing"
)

func TestNormalizeTextCleansEntitiesAndInvisibleRunes(t *testing.T) {
	raw := "Hello&nbsp;\u200bworld\r\n\r\nLine&#32;2"
	got := normalizeText(raw)
	want := "Hello world\n\nLine 2"
	if got != want {
		t.Fatalf("normalizeText() = %q, want %q", got, want)
	}
}

func TestRenderDisplayTextPrefersReadablePlain(t *testing.T) {
	plain := "Welcome to V-Claw.\nThis is a short update."
	html := "<p>Ignored fallback</p>"
	gotText, gotSource := renderDisplayText(plain, html, loadRenderConfig(0))

	if gotSource != displaySourcePlain {
		t.Fatalf("renderDisplayText() source = %q, want %q", gotSource, displaySourcePlain)
	}
	if gotText != normalizeText(plain) {
		t.Fatalf("renderDisplayText() text = %q, want %q", gotText, normalizeText(plain))
	}
}

func TestRenderDisplayTextFallsBackToHTMLWhenPlainIsNoisy(t *testing.T) {
	plain := "Click https://c.gle/1 https://c.gle/2 https://c.gle/3 https://c.gle/4 https://c.gle/5 https://c.gle/6 https://c.gle/7 https://c.gle/8 https://c.gle/9 https://c.gle/10"
	html := `<html><body><h1>Welcome</h1><p>Use your new tools.</p><a href="https://example.com/start">Start now</a></body></html>`
	gotText, gotSource := renderDisplayText(plain, html, loadRenderConfig(0))

	if gotSource != displaySourceHTMLFallback {
		t.Fatalf("renderDisplayText() source = %q, want %q", gotSource, displaySourceHTMLFallback)
	}
	if !strings.Contains(gotText, "Welcome") || !strings.Contains(gotText, "Start now (https://example.com/start)") {
		t.Fatalf("renderDisplayText() fallback output missing key content: %q", gotText)
	}
}

func TestHTMLToStructuredTextSkipsNoiseAndKeepsLinks(t *testing.T) {
	rawHTML := `
<!doctype html>
<html>
  <head>
    <style>.hero{color:red}</style>
    <script>alert("x")</script>
    <meta charset="utf-8" />
  </head>
  <body>
    <!-- comment -->
    <h1>Welcome to Workspace</h1>
    <p>Discover more <a href="https://example.com/discover">here</a>.</p>
    <ul><li>Item one</li><li>Item two</li></ul>
    <img alt="Hero image" src="https://img.example.com/a.png" />
    <form><input value="hidden" /></form>
  </body>
</html>`

	got := htmlToStructuredText(rawHTML)

	if strings.Contains(strings.ToLower(got), "alert(") || strings.Contains(got, ".hero") {
		t.Fatalf("htmlToStructuredText() should not include style/script noise: %q", got)
	}
	if !strings.Contains(got, "Welcome to Workspace") {
		t.Fatalf("htmlToStructuredText() missing heading: %q", got)
	}
	if !strings.Contains(got, "here (https://example.com/discover)") {
		t.Fatalf("htmlToStructuredText() missing link conversion: %q", got)
	}
	if !strings.Contains(got, "- Item one") {
		t.Fatalf("htmlToStructuredText() missing list item: %q", got)
	}
	if !strings.Contains(got, "[Image: Hero image]") {
		t.Fatalf("htmlToStructuredText() missing image alt: %q", got)
	}
}

func TestPreviewTextTruncates(t *testing.T) {
	got, truncated := previewText("1234567890", 5)
	if !strings.Contains(got, "[truncated]") || !truncated {
		t.Fatalf("previewText() should include truncation marker: %q", got)
	}
}

func TestGoldenMarketingLikeHTML(t *testing.T) {
	rawHTML := `
<html><head><style>table{border-collapse:collapse}</style><script>console.log(1)</script></head>
<body>
  <h1>Welcome to a better way of working</h1>
  <p>Discover how leading teams are using Google Workspace to connect.</p>
  <p><a href="https://c.gle/AOExmqX">View your recommendations</a></p>
  <table><tr><td>Footer</td><td>Google LLC</td></tr></table>
</body></html>`

	got := htmlToStructuredText(rawHTML)
	if strings.Contains(got, "console.log") || strings.Contains(got, "border-collapse") {
		t.Fatalf("golden output should strip style/script noise: %q", got)
	}
	if !strings.Contains(got, "Welcome to a better way of working") {
		t.Fatalf("golden output missing headline: %q", got)
	}
	if !strings.Contains(got, "View your recommendations (https://c.gle/AOExmqX)") {
		t.Fatalf("golden output missing CTA link: %q", got)
	}
	if !strings.Contains(got, "Footer") || !strings.Contains(got, "Google LLC") {
		t.Fatalf("golden output missing footer content: %q", got)
	}
}
