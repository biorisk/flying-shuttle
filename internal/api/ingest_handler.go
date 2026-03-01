package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/ingest/embedfile"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/google/uuid"
)

type ingestHandler struct {
	store    store.Store
	idx      *search.HybridIndex
	hnswPath string // path for HNSW persistence; empty to skip saving
}

// POST /api/v1/ingest/embed-file
// Body: {"path": "/absolute/path/to/file.fembed"}
// Response: {"imported": N}
func (h *ingestHandler) importEmbedFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	n, err := h.importFile(req.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
}

// POST /api/v1/ingest/directory
// Body: {"path": "/absolute/path/to/dir"}
// Response: {"files": N, "imported": M}
func (h *ingestHandler) importDirectory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	matches, err := filepath.Glob(filepath.Join(req.Path, "*.fembed"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "glob error: "+err.Error())
		return
	}

	totalImported := 0
	for _, path := range matches {
		n, err := h.importFile(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("import %s: %v", path, err))
			return
		}
		totalImported += n
	}

	writeJSON(w, http.StatusOK, map[string]any{"files": len(matches), "imported": totalImported})
}

// importFile reads a .fembed file, stores all chunks, and indexes them.
func (h *ingestHandler) importFile(path string) (int, error) {
	r, err := embedfile.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open embed file %q: %w", path, err)
	}
	defer r.Close()

	var chunks []model.Chunk
	for {
		rec, err := r.Next()
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

	h.idx.IndexChunks(chunks)

	if h.hnswPath != "" {
		if err := h.idx.Vector.Save(h.hnswPath); err != nil {
			log.Printf("warning: failed to save HNSW index to %s: %v", h.hnswPath, err)
		}
	}

	return len(chunks), nil
}
