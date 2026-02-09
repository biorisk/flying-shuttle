package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type uploadHandler struct {
	store      store.Store
	uploadDir  string
	transcribe ingest.Transcriber
	chunker    *ingest.Chunker
}

// list returns all uploads.
func (h *uploadHandler) list(w http.ResponseWriter, r *http.Request) {
	uploads, err := h.store.ListUploads()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, uploads)
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

// create handles multipart file upload.
func (h *uploadHandler) create(w http.ResponseWriter, r *http.Request) {
	// 100 MB max
	const maxUpload = 100 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing or invalid file field")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".mp3": true, ".wav": true, ".m4a": true, ".ogg": true, ".flac": true, ".webm": true}
	if !allowed[ext] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported format: %s", ext))
		return
	}

	id := uuid.NewString()
	diskName := id + ext
	destPath := filepath.Join(h.uploadDir, diskName)

	dst, err := os.Create(destPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(destPath)
		writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	u := &model.Upload{
		ID:        id,
		Filename:  header.Filename,
		Format:    strings.TrimPrefix(ext, "."),
		SizeBytes: written,
		Status:    model.UploadStatusPending,
	}
	if err := h.store.CreateUpload(u); err != nil {
		os.Remove(destPath)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Kick off transcription in background.
	go h.transcribeAsync(id, destPath)

	writeJSON(w, http.StatusCreated, u)
}

// transcribeAsync runs transcription and stores segments.
func (h *uploadHandler) transcribeAsync(uploadID, filePath string) {
	_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusTranscribing, "")

	ctx := context.Background()
	result, err := h.transcribe.Transcribe(ctx, filePath)
	if err != nil {
		_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, err.Error())
		return
	}

	for i := range result.Segments {
		seg := &result.Segments[i]
		seg.ID = uuid.NewString()
		seg.UploadID = uploadID
		if err := h.store.CreateTranscriptSegment(seg); err != nil {
			_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, err.Error())
			return
		}
	}

	// Run semantic chunking on the stored segments.
	if h.chunker != nil && len(result.Segments) > 0 {
		chunks, err := h.chunker.ChunkSegments(ctx, filePath, result.Segments)
		if err != nil {
			_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "chunking: "+err.Error())
			return
		}
		for i := range chunks {
			if err := h.store.CreateChunk(&chunks[i]); err != nil {
				_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "store chunk: "+err.Error())
				return
			}
		}
	}

	_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusDone, "")
}

// rechunk triggers semantic chunking for an upload's transcript segments.
func (h *uploadHandler) rechunk(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := h.store.GetUpload(id)
	if err != nil {
		writeError(w, errorStatus(err), err.Error())
		return
	}

	segs, err := h.store.ListTranscriptSegments(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(segs) == 0 {
		writeError(w, http.StatusBadRequest, "no transcript segments to chunk")
		return
	}

	chunks, err := h.chunker.ChunkSegments(r.Context(), u.Filename, segs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for i := range chunks {
		if err := h.store.CreateChunk(&chunks[i]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
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
