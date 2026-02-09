package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
)

type stitchHandler struct {
	store    store.Store
	stitcher stitch.Stitcher
}

func (h *stitchHandler) stitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChunkIDs  []string `json:"chunk_ids"`
		GlueLevel int      `json:"glue_level"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(body.ChunkIDs) == 0 {
		writeError(w, http.StatusBadRequest, "chunk_ids required")
		return
	}

	// Look up chunks from the store to get their content.
	chunks := make([]stitch.ChunkInput, 0, len(body.ChunkIDs))
	for _, id := range body.ChunkIDs {
		c, err := h.store.GetChunk(id)
		if err != nil {
			writeError(w, errorStatus(err), "chunk "+id+": "+err.Error())
			return
		}
		speaker := ""
		if c.Speaker != nil {
			speaker = *c.Speaker
		}
		chunks = append(chunks, stitch.ChunkInput{
			ID:      c.ID,
			Content: c.Content,
			Speaker: speaker,
		})
	}

	result, err := h.stitcher.Stitch(r.Context(), stitch.Request{
		Chunks:    chunks,
		GlueLevel: body.GlueLevel,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
