package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type uploadHandler struct {
	store       store.Store
	uploadDir   string
	index       *search.HybridIndex
	afterIngest func() // optional; nudges the embedding backfiller
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

// create handles multipart upload of a plain-text transcript. Audio is no
// longer supported — only .txt/.md/.markdown/.text files are accepted.
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
	if !ingest.IsTextTranscript(ext) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported format: %s (transcripts only: .txt, .md, .markdown, .text)", ext))
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

	// With ?defer=1 (batch uploads) the file is only persisted here; the
	// caller starts processing via POST /uploads/process once every file in
	// the batch is on disk. Otherwise processing begins immediately.
	if r.FormValue("defer") == "" {
		h.startProcessing(u)
	}

	writeJSON(w, http.StatusCreated, u)
}

// diskPath returns the on-disk location of an upload's file.
func (h *uploadHandler) diskPath(u *model.Upload) string {
	return filepath.Join(h.uploadDir, u.ID+"."+u.Format)
}

// startProcessing kicks off transcript ingestion for an upload in the
// background.
func (h *uploadHandler) startProcessing(u *model.Upload) {
	go h.ingestTranscriptAsync(u.ID, h.diskPath(u), u.Filename)
}

// process starts processing for a batch of already-uploaded files. Body:
// {"ids": [...]}; an empty or omitted list processes every pending upload.
// Only uploads still in the "pending" state are started, so repeated calls
// are safe.
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

	ids := req.IDs
	if len(ids) == 0 {
		all, err := h.store.ListUploads()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, u := range all {
			if u.Status == model.UploadStatusPending {
				ids = append(ids, u.ID)
			}
		}
	}

	started := 0
	for _, id := range ids {
		u, err := h.store.GetUpload(id)
		if err != nil || u.Status != model.UploadStatusPending {
			continue
		}
		h.startProcessing(u)
		started++
	}

	writeJSON(w, http.StatusOK, map[string]int{"started": started})
}

// ingestTranscriptAsync ingests an already-written plain-text transcript:
// it parses the file into segments, chunks them, and indexes the chunks.
func (h *uploadHandler) ingestTranscriptAsync(uploadID, filePath, sourceName string) {
	_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusTranscribing, "")

	raw, err := os.ReadFile(filePath)
	if err != nil {
		_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "read transcript: "+err.Error())
		return
	}

	segments := ingest.ParseTranscript(string(raw))
	if len(segments) == 0 {
		_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "transcript is empty")
		return
	}

	for i := range segments {
		seg := &segments[i]
		seg.ID = uuid.NewString()
		seg.UploadID = uploadID
		if err := h.store.CreateTranscriptSegment(seg); err != nil {
			_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "store segment: "+err.Error())
			return
		}
	}

	chunks := ingest.ChunkTranscript(sourceName, segments)
	if err := h.storeAndIndexChunks(chunks); err != nil {
		_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, err.Error())
		return
	}

	_ = h.store.UpdateUploadStatus(uploadID, model.UploadStatusDone, "")
}

// storeAndIndexChunks persists chunks, adds them to the search index, and
// nudges the embedding backfiller.
func (h *uploadHandler) storeAndIndexChunks(chunks []model.Chunk) error {
	for i := range chunks {
		if err := h.store.CreateChunk(&chunks[i]); err != nil {
			return fmt.Errorf("store chunk: %w", err)
		}
		if h.index != nil {
			h.index.IndexChunk(&chunks[i])
		}
	}
	if len(chunks) > 0 && h.afterIngest != nil {
		h.afterIngest()
	}
	return nil
}

// rechunk re-runs transcript chunking for an upload's stored segments and
// indexes the resulting chunks.
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

	chunks := ingest.ChunkTranscript(u.Filename, segs)
	if err := h.storeAndIndexChunks(chunks); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
