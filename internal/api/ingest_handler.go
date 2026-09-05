package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/ingest/embedfile"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/google/uuid"
)

type ingestHandler struct {
	store corpus.Store
	idx   *search.HybridIndex
}

// POST /api/v1/ingest/embed-file
// Body: {"path": "/absolute/path/to/file.fembed"}
// Response: {"imported": N}
func (h *ingestHandler) importEmbedFile(w http.ResponseWriter, r *http.Request) {
	path := h.decodePath(w, r)
	if path == "" {
		return
	}
	reader, err := embedfile.Open(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("open %q: %v", path, err))
		return
	}
	n, err := h.importStream(reader, path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
}

// POST /api/v1/ingest/embed-file-legacy
// Body: {"path": "/absolute/path/to/file.embed"}
// Response: {"imported": N}
//
// Accepts the legacy TSV .embed format directly — no Python conversion needed.
func (h *ingestHandler) importLegacyEmbedFile(w http.ResponseWriter, r *http.Request) {
	path := h.decodePath(w, r)
	if path == "" {
		return
	}
	reader, err := embedfile.OpenTSV(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("open %q: %v", path, err))
		return
	}
	n, err := h.importStream(reader, path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
}

// POST /api/v1/ingest/directory
// Body: {"path": "/absolute/path/to/dir"}
// Response: {"files": N, "imported": M}
//
// Imports all *.fembed files in the directory.
func (h *ingestHandler) importDirectory(w http.ResponseWriter, r *http.Request) {
	h.importGlob(w, r, "*.fembed", func(path string) (embedfile.Streamer, error) {
		return embedfile.Open(path)
	})
}

// POST /api/v1/ingest/directory-legacy
// Body: {"path": "/absolute/path/to/dir"}
// Response: {"files": N, "imported": M}
//
// Imports all *.embed TSV files in the directory directly, without conversion.
func (h *ingestHandler) importLegacyDirectory(w http.ResponseWriter, r *http.Request) {
	h.importGlob(w, r, "*.embed", func(path string) (embedfile.Streamer, error) {
		return embedfile.OpenTSV(path)
	})
}

// importGlob handles the shared logic for directory-wide ingest endpoints.
func (h *ingestHandler) importGlob(w http.ResponseWriter, r *http.Request, pattern string, open func(string) (embedfile.Streamer, error)) {
	path := h.decodePath(w, r)
	if path == "" {
		return
	}

	matches, err := filepath.Glob(filepath.Join(path, pattern))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "glob error: "+err.Error())
		return
	}

	totalImported := 0
	for _, p := range matches {
		reader, err := open(p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("open %s: %v", p, err))
			return
		}
		n, err := h.importStream(reader, p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("import %s: %v", p, err))
			return
		}
		totalImported += n
	}

	writeJSON(w, http.StatusOK, map[string]any{"files": len(matches), "imported": totalImported})
}

// importStream drains an embedfile.Streamer, stores all chunks, and indexes them.
func (h *ingestHandler) importStream(s embedfile.Streamer, path string) (int, error) {
	defer s.Close()

	var chunks []model.Chunk
	for {
		rec, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read record from %q: %w", path, err)
		}

		chunks = append(chunks, model.Chunk{
			ID:           uuid.NewString(),
			SourceFile:   rec.SourceFile,
			Content:      rec.Text,
			StartOffset:  int(rec.StartToken),
			EmbeddingVec: ingest.Float32sToBytes(rec.Embedding),
		})
	}

	if len(chunks) == 0 {
		return 0, nil
	}

	if err := h.store.CreateChunks(chunks); err != nil {
		return 0, fmt.Errorf("store chunks from %q: %w", path, err)
	}

	// IndexChunks marks the index dirty; the Snapshotter persists it.
	h.idx.IndexChunks(chunks)

	return len(chunks), nil
}

// decodePath decodes a {"path": "..."} JSON body and writes an error on failure.
// Returns "" if an error was written.
func (h *ingestHandler) decodePath(w http.ResponseWriter, r *http.Request) string {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return ""
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return ""
	}
	return req.Path
}
