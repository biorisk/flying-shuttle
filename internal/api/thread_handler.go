package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type threadHandler struct {
	store store.Store
}

func (h *threadHandler) list(w http.ResponseWriter, r *http.Request) {
	threads, err := h.store.ListThreads()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, threads)
}

func (h *threadHandler) create(w http.ResponseWriter, r *http.Request) {
	var t model.Thread
	if err := decodeJSON(r, &t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	t.ID = uuid.NewString()

	if err := h.store.CreateThread(&t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *threadHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := h.store.GetThread(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *threadHandler) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.store.GetThread(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}

	var input model.Thread
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	existing.Name = input.Name
	existing.Description = input.Description

	if err := h.store.UpdateThread(existing); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *threadHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.DeleteThread(id); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *threadHandler) getNodes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	nodes, err := h.store.GetThreadNodes(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (h *threadHandler) setNodes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Nodes []model.ThreadNode `json:"nodes"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Ensure thread_id is set correctly.
	for i := range body.Nodes {
		body.Nodes[i].ThreadID = id
	}
	if err := h.store.SetThreadNodes(id, body.Nodes); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *threadHandler) render(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	nodes, err := dag.Linearize(h.store, id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}
