package api

import (
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
)

type suggestHandler struct {
	store      store.Store
	translator *search.QueryTranslator
}

// suggest translates a node's content into chunk suggestions via the hybrid index.
// Already-used chunks (associated with any node) are excluded from results.
func (h *suggestHandler) suggest(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	node, err := h.store.GetNode(nodeID)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}

	limit := 5
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Find parent title for context expansion.
	var parentTitle string
	inEdges, err := h.store.ListEdgesTo(nodeID)
	if err == nil && len(inEdges) > 0 {
		parent, err := h.store.GetNode(inEdges[0].FromNode)
		if err == nil {
			parentTitle = parent.Title
		}
	}

	// Build exclusion set from already-used chunks.
	excludeIDs := make(map[string]bool)
	usedIDs, err := h.store.ListUsedChunkIDs()
	if err == nil {
		for _, id := range usedIDs {
			excludeIDs[id] = true
		}
	}

	suggestions, err := h.translator.TranslateWithOpts(r.Context(), search.TranslateOpts{
		Title:       node.Title,
		Body:        node.Body,
		ParentTitle: parentTitle,
		Limit:       limit,
		ExcludeIDs:  excludeIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, suggestions)
}
