package api

import (
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/export"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
)

type exportHandler struct {
	store    store.Store
	stitcher stitch.Stitcher
}

// exportMarkdown handles POST /api/v1/export/markdown
func (h *exportHandler) exportMarkdown(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ThreadID  string `json:"thread_id"`
		GlueLevel int    `json:"glue_level"`
		Title     string `json:"title"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	result, err := export.GenerateMarkdown(h.store, h.stitcher, export.ExportRequest{
		ThreadID:  body.ThreadID,
		GlueLevel: body.GlueLevel,
		Title:     body.Title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// downloadMarkdown handles GET /api/v1/export/markdown/download?thread_id=&title=
// Returns raw markdown as a file download.
func (h *exportHandler) downloadMarkdown(w http.ResponseWriter, r *http.Request) {
	threadID := r.URL.Query().Get("thread_id")
	title := r.URL.Query().Get("title")
	if title == "" {
		title = "Manuscript"
	}

	glueLevel := 50
	if v := r.URL.Query().Get("glue"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			glueLevel = n
		}
	}
	result, err := export.GenerateMarkdown(h.store, h.stitcher, export.ExportRequest{
		ThreadID:  threadID,
		GlueLevel: glueLevel,
		Title:     title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	filename := export.Slugify(title) + ".md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(result.Content))
}
