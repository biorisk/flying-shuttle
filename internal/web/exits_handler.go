package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *handlers) mountExits(r chi.Router) {
	r.Get("/outline/nodes/{id}/exits", h.nodeExits)
	r.Post("/outline/nodes/{id}/exits", h.nodeExitCreate)
	r.Delete("/outline/edges/{edge}", h.nodeExitDelete)
}

// nodeExitsView builds the #exits-<id> model: this bullet's non-linear
// outgoing edges plus the other outline bullets it could link to.
func (h *handlers) nodeExitsView(nodeID string) viewmodel.NodeExits {
	vm := viewmodel.NodeExits{NodeID: nodeID}

	titles := map[string]string{}
	nodes, _ := h.d.Store.ListNodes()
	for _, n := range nodes {
		if n.Type == model.NodeTypeOutline {
			titles[n.ID] = n.Title
		}
	}

	edges, _ := h.d.Store.ListEdgesFrom(nodeID)
	linked := map[string]bool{nodeID: true}
	for _, e := range edges {
		if e.Type == model.EdgeTypeLinear {
			linked[e.ToNode] = true
			continue
		}
		cond := ""
		if e.Condition != nil {
			cond = *e.Condition
		}
		vm.Exits = append(vm.Exits, viewmodel.ExitRow{
			EdgeID: e.ID, ToID: e.ToNode, ToTitle: titles[e.ToNode],
			Type: string(e.Type), Condition: cond,
		})
		linked[e.ToNode] = true
	}
	for id, title := range titles {
		if !linked[id] {
			vm.Options = append(vm.Options, viewmodel.ExitOption{ID: id, Title: title})
		}
	}
	return vm
}

func (h *handlers) nodeExits(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := Patch(w, r, components.Exits(h.nodeExitsView(id))); err != nil {
		log.Printf("exits: patch: %v", err)
	}
}

func (h *handlers) nodeExitCreate(w http.ResponseWriter, r *http.Request) {
	from := chi.URLParam(r, "id")
	_ = r.ParseForm()
	to := r.FormValue("to_node")
	etype := r.FormValue("type")
	if etype != "jump" {
		etype = "branch"
	}
	if to == "" || to == from {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}

	if cyc, err := dag.WouldCreateCycle(h.d.Store, from, to); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if cyc {
		http.Error(w, "edge would create a cycle", http.StatusConflict)
		return
	}

	e := &model.Edge{ID: uuid.NewString(), FromNode: from, ToNode: to, Type: model.EdgeType(etype)}
	if c := r.FormValue("condition"); c != "" {
		e.Condition = &c
	}
	if err := h.d.Store.CreateEdge(e); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := Patch(w, r, components.Exits(h.nodeExitsView(from))); err != nil {
		log.Printf("exits: patch: %v", err)
	}
}

func (h *handlers) nodeExitDelete(w http.ResponseWriter, r *http.Request) {
	edgeID := chi.URLParam(r, "edge")
	node := r.URL.Query().Get("node")
	if err := h.d.Store.DeleteEdge(edgeID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := Patch(w, r, components.Exits(h.nodeExitsView(node))); err != nil {
		log.Printf("exits: patch: %v", err)
	}
}
