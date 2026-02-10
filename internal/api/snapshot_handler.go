package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
)

type snapshotHandler struct {
	store store.Store
}

func (h *snapshotHandler) list(w http.ResponseWriter, r *http.Request) {
	snapshots, err := h.store.ListSnapshots()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

func (h *snapshotHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	summary, err := h.store.CreateSnapshot(body.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

func (h *snapshotHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	snap, err := h.store.GetSnapshot(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (h *snapshotHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.DeleteSnapshot(id); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *snapshotHandler) restore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.RestoreSnapshot(id); err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
