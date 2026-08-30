package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/pipeline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
)

type uploadHandler struct {
	store    store.Store
	ingester *pipeline.Ingester
}

// list returns a page of uploads (newest first) with pagination metadata.
func (h *uploadHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r)
	uploads, total, err := h.store.ListUploadsPage(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writePage(w, http.StatusOK, uploads, total, limit, offset)
}

// get returns a single upload by ID.
func (h *uploadHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := h.store.GetUpload(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// create handles multipart upload of a plain-text transcript.
func (h *uploadHandler) create(w http.ResponseWriter, r *http.Request) {
	const maxUpload = 100 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing or invalid file field")
		return
	}
	defer file.Close()

	u, err := h.ingester.Accept(header.Filename, file)
	if err != nil {
		var unsupported pipeline.ErrUnsupportedFormat
		if errors.As(err, &unsupported) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// With ?defer=1 (batch uploads) the file is only persisted here; the
	// caller starts processing via POST /uploads/process once every file is
	// on disk. Otherwise processing begins immediately.
	if r.FormValue("defer") == "" {
		h.ingester.Start(u)
	}

	writeJSON(w, http.StatusCreated, u)
}

// process starts processing for a batch of already-uploaded files. Body:
// {"ids": [...]}; an empty or omitted list processes every pending upload.
func (h *uploadHandler) process(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}

	started, err := h.ingester.StartPending(req.IDs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"started": started})
}

// rechunk re-runs transcript chunking for an upload's stored segments.
func (h *uploadHandler) rechunk(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	chunks, err := h.ingester.Rechunk(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, chunks)
}

// listSegments returns transcript segments for an upload.
func (h *uploadHandler) listSegments(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	segs, err := h.store.ListTranscriptSegments(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, segs)
}
