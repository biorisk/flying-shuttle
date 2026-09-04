package web

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	"github.com/go-chi/chi/v5"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// mountAtlas wires the Source Atlas fragment endpoints. The Atlas is the
// derived NETWORK over the transcript corpus (regions + similarity links) —
// it is not the outline.
func (h *handlers) mountAtlas(r chi.Router) {
	if h.d.Atlas == nil {
		return
	}
	r.Get("/atlas", h.atlasPane)
	r.Get("/atlas/status", h.atlasStatus)
	r.Get("/atlas/search", h.atlasSearch)
	r.Get("/atlas/regions/{id}", h.atlasRegion)
	r.Post("/atlas/rebuild", h.atlasRebuild)
}

func atlasSignals(st atlas.Status) map[string]any {
	return map[string]any{
		"atlasBuilding":   st.Building,
		"atlasRegions":    st.Regions,
		"atlasChunkCount": st.ChunkCount,
		"atlasError":      st.LastError,
	}
}

// atlasPaneView assembles the #atlas fragment model from the service state.
func (h *handlers) atlasPaneView() viewmodel.AtlasPane {
	svc := h.d.Atlas
	st := svc.Status()
	vm := viewmodel.AtlasPane{Error: st.LastError, Building: st.Building, ChunkCount: st.ChunkCount}

	build, _ := svc.Current()
	switch {
	case st.Building:
		vm.Status = "building"
	case st.LastError != "" && build == nil:
		vm.Status = "failed"
	case build == nil:
		vm.Status = "none"
	default:
		vm.Status = "ready"
		vm.ChunkCount = build.ChunkCount
		for _, r := range build.Regions {
			vm.Regions = append(vm.Regions, viewmodel.AtlasRegionRow{
				ID: r.ID, Title: r.Digest.Title, Keywords: r.Digest.Keywords, ChunkCount: r.ChunkCount,
			})
		}
		if cur, err := h.d.Store.ListChunkIDsWithEmbedding(); err == nil {
			if behind := len(cur) - build.ChunkCount; behind > 0 && behind*10 > build.ChunkCount {
				vm.Stale, vm.Behind = true, behind
			}
		}
	}
	return vm
}

// atlasPaneSSR returns the pane model for the initial shell render, or an
// empty (Status "none") pane when the Atlas service isn't wired.
func (h *handlers) atlasPaneSSR() viewmodel.AtlasPane {
	if h.d.Atlas == nil {
		return viewmodel.AtlasPane{Status: "none"}
	}
	return h.atlasPaneView()
}

func (h *handlers) atlasPane(w http.ResponseWriter, r *http.Request) {
	vm := h.atlasPaneView()
	// When opened with a bullet in focus, seed the "sources for this bullet"
	// ranking so it's there before the user searches.
	if node := r.URL.Query().Get("node"); node != "" && vm.Status == "ready" {
		vm.Matches = h.atlasAffinityFor(r.Context(), node)
	}
	if _, err := Patch(w, r, components.Atlas(vm)); err != nil {
		log.Printf("atlas pane: %v", err)
	}
}

// atlasAffinityFor ranks regions against a bullet's prose.
func (h *handlers) atlasAffinityFor(ctx context.Context, nodeID string) viewmodel.AtlasMatches {
	n, err := h.d.Store.GetNode(nodeID)
	if err != nil {
		return viewmodel.AtlasMatches{}
	}
	text := strings.TrimSpace(n.Title + "\n" + n.Body)
	hits := h.d.Atlas.RankForText(ctx, text, 5)
	return h.matchesView("sources for this bullet", hits)
}

func (h *handlers) matchesView(label string, hits []atlas.RegionHit) viewmodel.AtlasMatches {
	m := viewmodel.AtlasMatches{Label: label}
	for _, hit := range hits {
		if hit.Score <= 0 {
			continue
		}
		if reg := h.d.Atlas.Region(hit.RegionID); reg != nil {
			m.Regions = append(m.Regions, viewmodel.AtlasRegionRow{
				ID: reg.ID, Title: reg.Digest.Title, Keywords: reg.Digest.Keywords, ChunkCount: reg.ChunkCount,
			})
		}
	}
	return m
}

func (h *handlers) atlasSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var m viewmodel.AtlasMatches
	if q != "" {
		m = h.matchesView("“"+q+"”", h.d.Atlas.RankForText(r.Context(), q, 8))
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.AtlasMatches(m)); err != nil {
		log.Printf("atlas search: %v", err)
	}
}

func (h *handlers) atlasStatus(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
}

func (h *handlers) atlasRebuild(w http.ResponseWriter, r *http.Request) {
	h.d.Atlas.StartRebuild()
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(components.Atlas(h.atlasPaneView()))
	_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
}

func (h *handlers) atlasRegion(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	build, _ := h.d.Atlas.Current()
	if build == nil {
		http.Error(w, "no atlas build", http.StatusNotFound)
		return
	}

	var region *atlas.Region
	for i := range build.Regions {
		if build.Regions[i].ID == id {
			region = &build.Regions[i]
			break
		}
	}
	if region == nil {
		http.Error(w, "region not found", http.StatusNotFound)
		return
	}

	vm := viewmodel.AtlasRegionDetail{
		ID: region.ID, Title: region.Digest.Title, Abstract: region.Digest.Abstract,
		Keywords: region.Digest.Keywords, Source: region.Digest.Source,
	}
	for _, m := range region.Members {
		c, err := h.d.Store.GetChunk(m.ChunkID)
		if err != nil {
			continue
		}
		vm.Members = append(vm.Members, viewmodel.Candidate{
			ChunkID:    c.ID,
			SourceFile: c.SourceFile,
			Snippet:    trimRunes(c.Content, snippetRunes),
		})
	}
	titleByID := map[string]string{}
	for i := range build.Regions {
		titleByID[build.Regions[i].ID] = build.Regions[i].Digest.Title
	}
	for _, l := range build.Links { // build.Links is sorted strongest-first
		var other string
		switch id {
		case l.RegionA:
			other = l.RegionB
		case l.RegionB:
			other = l.RegionA
		default:
			continue
		}
		vm.Neighbours = append(vm.Neighbours, viewmodel.AtlasRegionRow{ID: other, Title: titleByID[other]})
	}

	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.AtlasRegion(vm)); err != nil {
		log.Printf("atlas region: %v", err)
	}
}
