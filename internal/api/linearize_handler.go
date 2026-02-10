package api

import (
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
)

type linearizeHandler struct {
	store    store.Store
	stitcher stitch.Stitcher
}

// linearizeThread handles GET /api/v1/threads/{id}/linearize?glue_level=50
func (h *linearizeHandler) linearizeThread(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	glueLevel := parseGlueLevel(r)

	result, err := dag.LinearizeAndStitch(r.Context(), h.store, h.stitcher, dag.LinearizeRequest{
		Mode:      dag.ModeThread,
		ThreadID:  threadID,
		GlueLevel: glueLevel,
	})
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// linearizeManuscript handles GET /api/v1/dag/linearize?glue_level=50
func (h *linearizeHandler) linearizeManuscript(w http.ResponseWriter, r *http.Request) {
	glueLevel := parseGlueLevel(r)

	result, err := dag.LinearizeAndStitch(r.Context(), h.store, h.stitcher, dag.LinearizeRequest{
		Mode:      dag.ModeManuscript,
		GlueLevel: glueLevel,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseGlueLevel(r *http.Request) int {
	s := r.URL.Query().Get("glue_level")
	if s == "" {
		return 50
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 || v > 100 {
		return 50
	}
	return v
}
