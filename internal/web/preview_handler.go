package web

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/biorisk/flying-shuttle/internal/export"
	"github.com/biorisk/flying-shuttle/internal/mdrender"
	"github.com/go-chi/chi/v5"
)

// mountPreview registers the project-preview routes: the working-doc mirror
// and the stitched manuscript, each viewable as rendered HTML, raw markdown,
// or PDF. The HTML page carries the width presets and format tabs.
func (h *handlers) mountPreview(r chi.Router) {
	r.Get("/outline.html", h.previewOutlineHTML)
	r.Get("/outline.md", h.previewOutlineRaw)
	r.Get("/outline.pdf", h.previewOutlinePDF)
	r.Get("/export.html", h.previewManuscriptHTML)
	r.Get("/export.pdf", h.previewManuscriptPDF)
	r.Get("/preview.events", h.previewEvents)
}

// --- outline (the working-doc mirror on disk) ---

func (h *handlers) outlineSource() ([]byte, error) {
	if h.d.OutlineMDPath == "" {
		return []byte("_(no working-doc mirror configured)_\n"), nil
	}
	b, err := os.ReadFile(h.d.OutlineMDPath)
	if os.IsNotExist(err) {
		return []byte("_(outline.md not written yet — make a change)_\n"), nil
	}
	return b, err
}

func (h *handlers) previewOutlineHTML(w http.ResponseWriter, r *http.Request) {
	src, err := h.outlineSource()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, err := mdrender.RenderHTML(mdrender.Markdown(), src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mdrender.WritePage(w, mdrender.Page{
		Title:       "outline · " + orProject(h.d.ProjectName),
		Body:        body,
		Format:      "rendered",
		RenderedURL: "/outline.html",
		RawURL:      "/outline.md",
		PDFURL:      "/outline.pdf",
		LiveURL:     "/preview.events",
		BackURL:     "/",
		BackLabel:   "editor",
	})
}

func (h *handlers) previewOutlineRaw(w http.ResponseWriter, r *http.Request) {
	src, err := h.outlineSource()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(src)
}

func (h *handlers) previewOutlinePDF(w http.ResponseWriter, r *http.Request) {
	src, err := h.outlineSource()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writePDF(w, src, "outline — "+orProject(h.d.ProjectName), "outline")
}

// --- manuscript (the stitched export) ---

func (h *handlers) manuscriptSource(r *http.Request) ([]byte, string, error) {
	q := r.URL.Query()
	title := q.Get("title")
	if title == "" {
		title = "Manuscript"
	}
	glue := 50
	if v, err := strconv.Atoi(q.Get("glue")); err == nil {
		glue = v
	}
	res, err := export.GenerateMarkdown(h.d.Store, h.d.Stitcher, export.ExportRequest{
		ThreadID: q.Get("thread"), GlueLevel: glue, Title: title,
	})
	if err != nil {
		return nil, title, err
	}
	return []byte(res.Content), title, nil
}

func (h *handlers) previewManuscriptHTML(w http.ResponseWriter, r *http.Request) {
	src, title, err := h.manuscriptSource(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, err := mdrender.RenderHTML(mdrender.Markdown(), src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	qs := "?" + r.URL.RawQuery
	if r.URL.RawQuery == "" {
		qs = ""
	}
	mdrender.WritePage(w, mdrender.Page{
		Title:       title,
		Body:        body,
		Format:      "rendered",
		RenderedURL: "/export.html" + qs,
		RawURL:      "/export.md" + withParam(qs, "inline", "1"),
		PDFURL:      "/export.pdf" + qs,
		LiveURL:     "/preview.events",
		BackURL:     "/",
		BackLabel:   "editor",
	})
}

func (h *handlers) previewManuscriptPDF(w http.ResponseWriter, r *http.Request) {
	src, title, err := h.manuscriptSource(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writePDF(w, src, title, export.Slugify(title))
}

// --- shared ---

func writePDF(w http.ResponseWriter, src []byte, title, filenameStem string) {
	pdf, err := mdrender.RenderPDF(mdrender.Markdown(), src, title, "")
	if err != nil {
		log.Printf("preview: pdf: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	w.Header().Set("Content-Disposition", `inline; filename="`+filenameStem+`.pdf"`)
	_, _ = w.Write(pdf)
}

func orProject(name string) string {
	if name == "" {
		return "project"
	}
	return name
}

func withParam(qs, k, v string) string {
	if qs == "" {
		return "?" + k + "=" + v
	}
	return qs + "&" + k + "=" + v
}

// previewEvents is the live-reload stream for the preview pages: it emits a
// "reload" event each time the working-doc mirror is rewritten (i.e. the
// project state changed).
//
//	GET /preview.events
func (h *handlers) previewEvents(w http.ResponseWriter, r *http.Request) {
	if h.d.PreviewReload == nil {
		http.NotFound(w, r)
		return
	}
	ch, cancel := h.d.PreviewReload.subscribe()
	defer cancel()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // long-lived; opt out of the server WriteTimeout
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(": connected\n\n"))
	_ = rc.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if _, err := w.Write([]byte("event: reload\ndata: 1\n\n")); err != nil {
				return
			}
			_ = rc.Flush()
		}
	}
}
