package web

import (
	"log"
	"net/http"
	"time"

	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	"github.com/go-chi/chi/v5"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// --- snapshots ---

func (h *handlers) snapshotBarView() viewmodel.SnapshotBar {
	var vm viewmodel.SnapshotBar
	snaps, err := h.d.Store.ListSnapshots()
	if err != nil {
		log.Printf("snapshot bar: %v", err)
		return vm
	}
	for _, s := range snaps {
		vm.Snapshots = append(vm.Snapshots, viewmodel.SnapshotRow{
			ID: s.ID, Label: s.Label, Created: s.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return vm
}

func (h *handlers) snapshotBar(w http.ResponseWriter, r *http.Request) {
	if _, err := Patch(w, r, components.SnapshotBar(h.snapshotBarView())); err != nil {
		log.Printf("snapshot bar: patch: %v", err)
	}
}

func (h *handlers) snapshotCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	label := r.FormValue("label")
	if label == "" {
		label = time.Now().Format("Jan 2, 15:04")
	}
	if _, err := h.d.Store.CreateSnapshot(label); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.snapshotBar(w, r)
}

func (h *handlers) snapshotDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Store.DeleteSnapshot(chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.snapshotBar(w, r)
}

func (h *handlers) snapshotRestore(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Store.RestoreSnapshot(chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.patchOutlineAndBars(w, r)
}

// patchOutlineAndBars re-renders the outline plus the snapshot/branch bars —
// used after any op that rewrites the whole DAG (restore, branch switch).
func (h *handlers) patchOutlineAndBars(w http.ResponseWriter, r *http.Request) {
	ov, _ := h.outlineView()
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(components.Outline(ov))
	_ = sse.PatchElementTempl(components.SnapshotBar(h.snapshotBarView()))
	_ = sse.PatchElementTempl(components.BranchBar(h.branchBarView()))
	_ = sse.MarshalAndPatchSignals(map[string]any{"focusId": "", "readerChunk": ""})
}

// --- branches ---

func (h *handlers) branchBarView() viewmodel.BranchBar {
	var vm viewmodel.BranchBar
	branches, err := h.d.Store.ListBranches()
	if err != nil {
		log.Printf("branch bar: %v", err)
		return vm
	}
	for _, b := range branches {
		vm.Branches = append(vm.Branches, viewmodel.BranchRow{ID: b.ID, Name: b.Name, Active: b.Active})
	}
	return vm
}

func (h *handlers) branchBar(w http.ResponseWriter, r *http.Request) {
	if _, err := Patch(w, r, components.BranchBar(h.branchBarView())); err != nil {
		log.Printf("branch bar: patch: %v", err)
	}
}

func (h *handlers) branchCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if _, err := h.d.Store.CreateBranch(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.branchBar(w, r)
}

func (h *handlers) branchSwitch(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Store.SwitchBranch(chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.patchOutlineAndBars(w, r)
}

func (h *handlers) branchDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Store.DeleteBranch(chi.URLParam(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.branchBar(w, r)
}
