package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	datastar "github.com/starfederation/datastar-go/datastar"
)

func (h *handlers) mountThreads(r chi.Router) {
	r.Get("/threads", h.threadBar)
	r.Post("/threads", h.threadCreate)
	r.Delete("/threads/{id}", h.threadDelete)
	r.Post("/threads/{id}/nodes/{node}/toggle", h.threadToggleNode)
	r.Post("/threads/{id}/nodes/{node}/append", h.threadAppendNode)
}

func (h *handlers) threadBarView() viewmodel.ThreadBar {
	var vm viewmodel.ThreadBar
	threads, err := h.d.Store.ListThreads()
	if err != nil {
		log.Printf("thread bar: %v", err)
		return vm
	}
	for _, t := range threads {
		vm.Threads = append(vm.Threads, viewmodel.ThreadRow{ID: t.ID, Name: t.Name})
	}
	return vm
}

func (h *handlers) threadBar(w http.ResponseWriter, r *http.Request) {
	if _, err := Patch(w, r, components.ThreadBar(h.threadBarView())); err != nil {
		log.Printf("thread bar: patch: %v", err)
	}
}

func (h *handlers) threadCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	t := &model.Thread{ID: uuid.NewString(), Name: name}
	if err := h.d.Store.CreateThread(t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Re-render the bar and select the new thread.
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(components.ThreadBar(h.threadBarView()))
	_ = sse.MarshalAndPatchSignals(map[string]any{"threadId": t.ID})
}

func (h *handlers) threadDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.d.Store.DeleteThread(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(components.ThreadBar(h.threadBarView()))
	_ = sse.MarshalAndPatchSignals(map[string]any{"threadId": ""})
}

// threadToggleNode adds or removes a node from a thread, then re-renders the
// outline scoped to that thread.
func (h *handlers) threadToggleNode(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "node")

	tns, err := h.d.Store.GetThreadNodes(threadID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	next := tns[:0]
	found := false
	for _, tn := range tns {
		if tn.NodeID == nodeID {
			found = true
			continue
		}
		next = append(next, tn)
	}
	if !found {
		next = append(next, model.ThreadNode{ThreadID: threadID, NodeID: nodeID})
	}
	renumber(next)
	if err := h.d.Store.SetThreadNodes(threadID, next); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.patchThreadOutline(w, r, threadID)
}

// threadAppendNode appends a node to the end of a thread's path (brush mode).
func (h *handlers) threadAppendNode(w http.ResponseWriter, r *http.Request) {
	threadID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "node")

	tns, err := h.d.Store.GetThreadNodes(threadID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, tn := range tns {
		if tn.NodeID == nodeID {
			h.patchThreadOutline(w, r, threadID) // already present — no change
			return
		}
	}
	tns = append(tns, model.ThreadNode{ThreadID: threadID, NodeID: nodeID})
	renumber(tns)
	if err := h.d.Store.SetThreadNodes(threadID, tns); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.patchThreadOutline(w, r, threadID)
}

func (h *handlers) patchThreadOutline(w http.ResponseWriter, r *http.Request, threadID string) {
	ov, _ := h.outlineViewFor(threadID)
	if _, err := Patch(w, r, components.Outline(ov)); err != nil {
		log.Printf("thread outline: patch: %v", err)
	}
}

func renumber(tns []model.ThreadNode) {
	for i := range tns {
		tns[i].Position = i
	}
}
