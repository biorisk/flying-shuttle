package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
)

type contextHandler struct {
	store   store.Store
	checker *search.ContextChecker
}

// checkContext verifies whether a node's evidence chunks fit semantically
// within the given parent's context. Called after drag-and-drop to flag
// potentially incoherent moves.
func (h *contextHandler) checkContext(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")

	var body struct {
		ParentID string `json:"parent_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// If no parent specified (promoted to root), always in context.
	if body.ParentID == "" {
		writeJSON(w, http.StatusOK, &search.ContextCheck{InContext: true, Score: 1.0})
		return
	}

	// Look up parent node for its title.
	parent, err := h.store.GetNode(body.ParentID)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}

	// Get the moved node's associated chunks.
	chunks, err := h.store.GetNodeChunks(nodeID)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}

	// If node has no chunks, it's always in context (outline/synth node).
	if len(chunks) == 0 {
		writeJSON(w, http.StatusOK, &search.ContextCheck{InContext: true, Score: 1.0})
		return
	}

	chunkIDs := make([]string, len(chunks))
	for i, c := range chunks {
		chunkIDs[i] = c.ID
	}

	check, err := h.checker.Check(r.Context(), parent.Title, chunkIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, check)
}
