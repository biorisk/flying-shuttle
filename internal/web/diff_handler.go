package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/google/uuid"
)

// outlineRescue recreates a bullet that was removed since the diff baseline,
// re-linking it to its recorded parent, then re-renders the diffed outline.
//
//	POST /app/outline/rescue?diff=<baseline id>&node=<ghost node id>
func (h *handlers) outlineRescue(w http.ResponseWriter, r *http.Request) {
	baseID := r.URL.Query().Get("diff")
	ghostID := r.URL.Query().Get("node")
	if baseID == "" || ghostID == "" {
		http.Error(w, "diff and node required", http.StatusBadRequest)
		return
	}

	var g *outline.Ghost
	for _, gh := range h.computeOutlineDiff(baseID).Ghosts {
		if gh.Node.ID == ghostID {
			gh := gh
			g = &gh
			break
		}
	}
	if g == nil {
		http.Error(w, "ghost not found", http.StatusNotFound)
		return
	}

	n := &model.Node{
		ID: uuid.NewString(), Type: model.NodeTypeOutline,
		Title: g.Node.Title, Body: g.Node.Body, Locked: g.Node.Locked,
	}
	if err := h.d.Store.CreateNode(n); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if g.ParentID != "" {
		if err := h.d.Store.MoveNode(n.ID, g.ParentID, g.Weight); err != nil {
			log.Printf("rescue: move: %v", err)
		}
	}

	ov, _ := h.outlineViewOpts(outlineOpts{DiffAgainst: baseID})
	if _, err := Patch(w, r, components.Outline(ov)); err != nil {
		log.Printf("rescue: patch: %v", err)
	}
}
