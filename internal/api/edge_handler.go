package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type edgeHandler struct {
	store store.Store
}

func (h *edgeHandler) list(w http.ResponseWriter, r *http.Request) {
	edges, err := h.store.ListEdges()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, edges)
}

func (h *edgeHandler) create(w http.ResponseWriter, r *http.Request) {
	var e model.Edge
	if err := decodeJSON(r, &e); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	e.ID = uuid.NewString()
	if e.Type == "" {
		e.Type = model.EdgeTypeLinear
	}

	// Reject self-links.
	if e.FromNode == e.ToNode {
		writeError(w, http.StatusBadRequest, "self-links are not allowed")
		return
	}

	// Validate acyclicity before creating.
	cycle, err := dag.WouldCreateCycle(h.store, e.FromNode, e.ToNode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cycle {
		writeError(w, http.StatusConflict, "edge would create a cycle")
		return
	}

	if err := h.store.CreateEdge(&e); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (h *edgeHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := h.store.GetEdge(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *edgeHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.DeleteEdge(id); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
