package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/export"
)

// exportMarkdown streams the linearized manuscript as a downloadable .md file.
//
//	GET /export.md?thread=<id>&glue=<0-100>&title=<name>
func (h *handlers) exportMarkdown(w http.ResponseWriter, r *http.Request) {
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
		ThreadID:  q.Get("thread"),
		GlueLevel: glue,
		Title:     title,
	})
	if err != nil {
		log.Printf("export: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+export.Slugify(title)+`.md"`)
	_, _ = w.Write([]byte(res.Content))
}

// testReset deletes every node (edges/evidence/thread-memberships cascade) and
// every thread. Guarded by SHUTTLE_E2E — see Mount.
//
//	POST /_test/reset
func (h *handlers) testReset(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.d.Store.ListNodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, n := range nodes {
		_ = h.d.Store.DeleteNode(n.ID)
	}
	if threads, err := h.d.Store.ListThreads(); err == nil {
		for _, t := range threads {
			_ = h.d.Store.DeleteThread(t.ID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
