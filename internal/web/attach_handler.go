package web

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/go-chi/chi/v5"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// outlineAttachEvidence creates a locked evidence sub-bullet under the current
// bullet from the #excerpt-form fields, then re-renders the outline. Focus
// stays on the parent bullet and the transcript reader closes.
//
//	POST /outline/nodes/{id}/evidence
//	form: chunk_id, char_start?, char_end?, text?
func (h *handlers) outlineAttachEvidence(w http.ResponseWriter, r *http.Request) {
	parentID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	chunkID := r.FormValue("chunk_id")
	if chunkID == "" {
		http.Error(w, "chunk_id required", http.StatusBadRequest)
		return
	}
	start, _ := strconv.Atoi(r.FormValue("char_start"))
	end, _ := strconv.Atoi(r.FormValue("char_end"))
	text := r.FormValue("text")

	if _, err := h.d.Outline.AttachEvidence(parentID, chunkID, start, end, text); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("attach evidence: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ov, err := h.outlineView()
	if err != nil {
		log.Printf("attach evidence: view: %v", err)
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.Outline(ov)); err != nil {
		log.Printf("attach evidence: patch: %v", err)
		return
	}
	_ = sse.MarshalAndPatchSignals(map[string]any{
		"focusId":     parentID,
		"readerChunk": "",
	})
}
