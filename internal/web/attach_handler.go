package web

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/doc"
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
	if parentID == "" {
		http.Error(w, "no bullet in focus", http.StatusBadRequest)
		return
	}
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
		if errors.Is(err, doc.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("attach evidence: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderOutlineAfterAttach(w, r, parentID)
}

// outlineAttachEvidenceNewBullet creates a fresh empty bullet at the end of the
// outline and attaches the given passage to it as evidence — the "add
// selection (new bullet)" action, used when nothing is in focus (e.g. browsing
// the Source Atlas). Focus moves to the new bullet.
//
//	POST /outline/evidence-new
//	form: chunk_id, char_start?, char_end?, text?
func (h *handlers) outlineAttachEvidenceNewBullet(w http.ResponseWriter, r *http.Request) {
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

	n, err := h.d.Outline.AddRoot("")
	if err != nil {
		log.Printf("attach evidence (new bullet): add root: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := h.d.Outline.AttachEvidence(n.ID, chunkID, start, end, text); err != nil {
		if errors.Is(err, doc.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("attach evidence (new bullet): %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderOutlineAfterAttach(w, r, n.ID)
}

// renderOutlineAfterAttach re-renders #outline, focuses the given bullet and
// closes the transcript reader — the shared tail of both attach handlers.
func (h *handlers) renderOutlineAfterAttach(w http.ResponseWriter, r *http.Request, focusID string) {
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
		"focusId":     focusID,
		"readerChunk": "",
	})
}
