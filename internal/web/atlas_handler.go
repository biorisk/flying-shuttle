package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/go-chi/chi/v5"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// mountAtlas wires the Source Atlas endpoints. The rich explore/graph panes
// arrive in their own tasks; this is the rebuild trigger + status signals.
func (h *handlers) mountAtlas(r chi.Router) {
	if h.d.Atlas == nil {
		return
	}
	r.Get("/atlas/status", h.atlasStatus)
	r.Post("/atlas/rebuild", h.atlasRebuild)
}

func atlasSignals(st atlas.Status) map[string]any {
	return map[string]any{
		"atlasBuilding":   st.Building,
		"atlasRegions":    st.Regions,
		"atlasLinks":      st.Links,
		"atlasChunkCount": st.ChunkCount,
		"atlasError":      st.LastError,
	}
}

func (h *handlers) atlasStatus(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
}

func (h *handlers) atlasRebuild(w http.ResponseWriter, r *http.Request) {
	if started := h.d.Atlas.StartRebuild(); !started {
		log.Printf("atlas: rebuild already in progress")
	}
	sse := datastar.NewSSE(w, r)
	_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
}
