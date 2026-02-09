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

	suggestions, err := h.translator.Translate(r.Context(), node.Title, node.Body, parentTitle, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, suggestions)
}
