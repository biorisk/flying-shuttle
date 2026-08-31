package mdrender

import (
	"net/http/httptest"
	"strings"
	"testing"
)

const sample = "# Title\n\nSome **bold** text and a list:\n\n- one\n- two\n\n> a quote\n\n```go\nfmt.Println(\"hi\")\n```\n"

func TestRenderHTML(t *testing.T) {
	md := Markdown()
	h, err := RenderHTML(md, []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	s := string(h)
	for _, w := range []string{"<h1", "Title", "<strong>bold</strong>", "<blockquote>", "<pre", "Println"} {
		if !strings.Contains(s, w) {
			t.Fatalf("html missing %q:\n%s", w, s)
		}
	}
}

func TestRenderPDF(t *testing.T) {
	md := Markdown()
	b, err := RenderPDF(md, []byte(sample), "Title", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 500 || string(b[:4]) != "%PDF" {
		t.Fatalf("not a PDF: %d bytes, %q", len(b), b[:min(8, len(b))])
	}
}

func TestPreviewPageControls(t *testing.T) {
	md := Markdown()
	body, _ := RenderHTML(md, []byte(sample))
	rec := httptest.NewRecorder()
	WritePage(rec, Page{
		Title: "outline.md", Body: body, Format: "rendered",
		RenderedURL: "/outline.html", RawURL: "/outline.md", PDFURL: "/outline.pdf",
		LiveURL: "/preview.events", BackURL: "/", BackLabel: "app",
	})
	h := rec.Body.String()
	for _, w := range []string{
		`name="doc-width"`, `value="letter"`, `value="landscape"`, `value="fit"`,
		"shuttle-preview-width",
		`href="/outline.html"`, `class="active"`, `href="/outline.md"`, `href="/outline.pdf"`,
		"EventSource(", "preview.events",
		"&larr; app",
	} {
		if !strings.Contains(h, w) {
			t.Fatalf("preview page missing %q:\n%s", w, h)
		}
	}
}
