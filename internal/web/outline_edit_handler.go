package web

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/go-chi/chi/v5"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// mountOutlineEdit registers the structural-edit endpoints. Split out so the
// route list in Mount stays readable.
func (h *handlers) mountOutlineEdit(r chi.Router) {
	r.Post("/outline/roots", h.outlineAddRoot)
	r.Route("/outline/nodes/{id}", func(r chi.Router) {
		r.Patch("/", h.outlineSetTitle)
		r.Delete("/", h.outlineDelete)
		r.Post("/sibling", h.outlineAddSibling)
		r.Post("/child", h.outlineAddChild)
		r.Post("/indent", h.outlineIndent)
		r.Post("/unindent", h.outlineUnindent)
	})
}

// patchOutline re-renders #outline and, when focusID != "", also patches the
// focusId signal so the client focuses that bullet's input.
func (h *handlers) patchOutline(w http.ResponseWriter, r *http.Request, focusID string) {
	ov, err := h.outlineView()
	if err != nil {
		log.Printf("outline edit: view: %v", err)
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.Outline(ov)); err != nil {
		log.Printf("outline edit: patch elements: %v", err)
		return
	}
	if focusID != "" {
		if err := sse.MarshalAndPatchSignals(map[string]any{"focusId": focusID}); err != nil {
			log.Printf("outline edit: patch signals: %v", err)
		}
	}
}

func (h *handlers) outlineAddRoot(w http.ResponseWriter, r *http.Request) {
	n, err := h.d.Outline.AddRoot("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.patchOutline(w, r, n.ID)
}

// persistAnchorTitle saves the anchor bullet's current input value before a
// structural op, so text typed but not yet blurred isn't lost. Best-effort:
// version conflicts and missing fields are ignored.
func (h *handlers) persistAnchorTitle(r *http.Request, id string) {
	if err := r.ParseForm(); err != nil {
		return
	}
	if _, ok := r.Form["title"]; !ok {
		return
	}
	version, _ := strconv.Atoi(r.FormValue("version"))
	if _, err := h.d.Outline.SetTitle(id, r.FormValue("title"), version); err != nil && !errors.Is(err, store.ErrConflict) {
		log.Printf("outline edit: persist anchor title %s: %v", id, err)
	}
}

func (h *handlers) outlineAddSibling(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.persistAnchorTitle(r, id)
	n, err := h.d.Outline.AddSibling(id, "")
	if err != nil {
		h.editError(w, r, err)
		return
	}
	h.patchOutline(w, r, n.ID)
}

func (h *handlers) outlineAddChild(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.persistAnchorTitle(r, id)
	n, err := h.d.Outline.AddChild(id, "")
	if err != nil {
		h.editError(w, r, err)
		return
	}
	h.patchOutline(w, r, n.ID)
}

func (h *handlers) outlineIndent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.persistAnchorTitle(r, id)
	if _, err := h.d.Outline.Indent(id); err != nil && !errors.Is(err, outline.ErrNoop) {
		h.editError(w, r, err)
		return
	}
	h.patchOutline(w, r, id)
}

func (h *handlers) outlineUnindent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.persistAnchorTitle(r, id)
	if _, err := h.d.Outline.Unindent(id); err != nil && !errors.Is(err, outline.ErrNoop) {
		h.editError(w, r, err)
		return
	}
	h.patchOutline(w, r, id)
}

func (h *handlers) outlineDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	focus, _ := h.d.Outline.FocusAfterDelete(id)
	if err := h.d.Outline.Delete(id); err != nil {
		h.editError(w, r, err)
		return
	}
	h.patchOutline(w, r, focus)
}

// outlineSetTitle persists a bullet title on blur. It returns 204 on success
// (the input already shows the value) and only re-syncs #outline on a version
// conflict.
func (h *handlers) outlineSetTitle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := r.FormValue("title")
	version, _ := strconv.Atoi(r.FormValue("version"))

	_, err := h.d.Outline.SetTitle(id, title, version)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrConflict):
		// Client was stale — resend the whole outline.
		h.patchOutline(w, r, "")
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// editError maps store errors to statuses for the structural endpoints.
func (h *handlers) editError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		log.Printf("outline edit: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
