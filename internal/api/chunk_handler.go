package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type chunkHandler struct {
	store store.Store
}

func (h *chunkHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	chunks, total, err := h.store.ListChunksPage(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writePage(w, http.StatusOK, chunks, total, limit, offset)
}

func (h *chunkHandler) create(w http.ResponseWriter, r *http.Request) {
	var c model.Chunk
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	c.ID = uuid.NewString()

	if err := h.store.CreateChunk(&c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *chunkHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := h.store.GetChunk(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}
