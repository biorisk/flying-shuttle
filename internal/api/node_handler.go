package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type nodeHandler struct {
	store store.Store
}

func (h *nodeHandler) list(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (h *nodeHandler) create(w http.ResponseWriter, r *http.Request) {
	var n model.Node
	if err := decodeJSON(r, &n); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	n.ID = uuid.NewString()
	if n.Type == "" {
		n.Type = model.NodeTypeOutline
	}

	if err := h.store.CreateNode(&n); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (h *nodeHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	n, err := h.store.GetNode(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (h *nodeHandler) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.store.GetNode(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}

	var input model.Node
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	existing.Title = input.Title
	existing.Body = input.Body
	existing.Type = input.Type
	existing.Labels = input.Labels
	existing.Locked = input.Locked

	if err := h.store.UpdateNode(existing); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *nodeHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.DeleteNode(id); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *nodeHandler) getChunks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	chunks, err := h.store.GetNodeChunks(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chunks)
}

func (h *nodeHandler) setChunks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		ChunkIDs []string `json:"chunk_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.store.SetNodeChunks(id, body.ChunkIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *nodeHandler) move(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		ParentID string `json:"parent_id"`
		Position int    `json:"position"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.store.MoveNode(id, body.ParentID, body.Position); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *nodeHandler) getEdges(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	edges, err := h.store.ListEdgesFrom(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, edges)
}
