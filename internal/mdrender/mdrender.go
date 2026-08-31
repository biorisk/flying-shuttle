// Package mdrender renders Markdown to HTML and PDF for the project preview.
// The goldmark configuration and the PDF renderer (pdf.go) are lifted from the
// standalone `godown` tool; the preview page carries the same UI controls —
// width presets (letter / landscape / fit) and format tabs (rendered / raw /
// PDF).
package mdrender

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

//go:embed github.css
var css string

//go:embed page.html
var pageSrc string

var pageTmpl = template.Must(template.New("preview").Parse(pageSrc))

// Markdown returns the shared goldmark configuration: GitHub-flavored markdown,
// syntax highlighting, auto heading IDs, raw HTML passthrough.
func Markdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(highlighting.WithStyle("github")),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
}

// RenderHTML converts markdown source to an HTML fragment.
func RenderHTML(md goldmark.Markdown, src []byte) (template.HTML, error) {
	doc := md.Parser().Parse(text.NewReader(src))
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil //nolint:gosec // trusted local content
}

// Page is the render model for the standalone preview page.
type Page struct {
	Title  string        // <title> and the header name
	Body   template.HTML // rendered markdown
	Format string        // "rendered" (this page), plus links below
	// Format tab targets. Empty entries are omitted.
	RenderedURL string
	RawURL      string
	PDFURL      string
	// LiveURL, if set, is an SSE endpoint that emits "reload" events.
	LiveURL string
	// BackURL / BackLabel point to where "close" returns (the app).
	BackURL   string
	BackLabel string
}

func (p Page) css() template.CSS { return template.CSS(css) } //nolint:gosec // embedded asset

// WritePage renders the full preview HTML document.
func WritePage(w http.ResponseWriter, p Page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Page
		CSS template.CSS
	}{p, p.css()}
	_ = pageTmpl.Execute(w, data)
}
