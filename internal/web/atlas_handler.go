package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/ingest"
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
	r.Get("/atlas/graph.json", h.atlasGraphJSON)
	r.Get("/atlas/chunk/{id}", h.atlasChunk)
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
	if _, err := Patch(w, r, components.AtlasList(vm), components.AtlasCanvas(vm)); err != nil {
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
	vm := h.atlasPaneView()
	_ = sse.PatchElementTempl(components.AtlasList(vm))
	_ = sse.PatchElementTempl(components.AtlasCanvas(vm))
	_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
}

// --- network view (§6): graph JSON + chunk detail ---

type graphRegion struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Keywords []string `json:"keywords"`
	Chunks   int      `json:"chunks"`
}

type graphEdge struct {
	A string  `json:"a"`
	B string  `json:"b"`
	W float64 `json:"w"`
}

type graphChunk struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"`
}

// atlasGraphJSON serves the network-view payload. With ?region=<id> it returns
// that region's member chunks + intra-region similarity edges; otherwise the
// whole region graph. The client lays it out with a force sim — no coordinates
// are sent (see source_atlas_plan.md §12).
func (h *handlers) atlasGraphJSON(w http.ResponseWriter, r *http.Request) {
	build, _ := h.d.Atlas.Current()
	w.Header().Set("Content-Type", "application/json")
	if build == nil {
		json.NewEncoder(w).Encode(map[string]any{"regions": []any{}, "links": []any{}})
		return
	}

	if rid := r.URL.Query().Get("region"); rid != "" {
		h.writeRegionGraph(w, build, rid)
		return
	}

	regions := make([]graphRegion, 0, len(build.Regions))
	for _, reg := range build.Regions {
		regions = append(regions, graphRegion{
			ID: reg.ID, Title: reg.Digest.Title, Keywords: reg.Digest.Keywords, Chunks: reg.ChunkCount,
		})
	}
	links := make([]graphEdge, 0, len(build.Links))
	for _, l := range build.Links {
		links = append(links, graphEdge{A: l.RegionA, B: l.RegionB, W: l.Weight})
	}
	json.NewEncoder(w).Encode(map[string]any{"regions": regions, "links": links})
}

func (h *handlers) writeRegionGraph(w http.ResponseWriter, build *atlas.Build, rid string) {
	var region *atlas.Region
	for i := range build.Regions {
		if build.Regions[i].ID == rid {
			region = &build.Regions[i]
		}
	}
	if region == nil {
		http.Error(w, "region not found", http.StatusNotFound)
		return
	}

	ids := make([]string, len(region.Members))
	for i, m := range region.Members {
		ids[i] = m.ChunkID
	}
	chunks, _ := h.d.Store.GetChunksByIDs(ids)

	nodes := make([]graphChunk, 0, len(chunks))
	vecs := make(map[string][]float32, len(chunks))
	byID := map[string]bool{}
	for i := range chunks {
		c := &chunks[i]
		byID[c.ID] = true
		nodes = append(nodes, graphChunk{ID: c.ID, Label: chunkLabel(c.Content), Source: c.SourceFile})
		if len(c.EmbeddingVec) > 0 {
			vecs[c.ID] = ingest.BytesToFloat32s(c.EmbeddingVec)
		}
	}
	// Per-chunk keyword labels are better than a text head when we have them.
	for _, m := range region.Members {
		if len(m.Keywords) > 0 && byID[m.ChunkID] {
			for i := range nodes {
				if nodes[i].ID == m.ChunkID {
					nodes[i].Label = strings.Join(m.Keywords, " · ")
				}
			}
		}
	}

	var edges []graphEdge
	const simThreshold = 0.35
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			a, b := vecs[nodes[i].ID], vecs[nodes[j].ID]
			if a == nil || b == nil {
				continue
			}
			if s := ingest.CosineSimilarity(a, b); s >= simThreshold {
				edges = append(edges, graphEdge{A: nodes[i].ID, B: nodes[j].ID, W: s})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"region": map[string]string{"id": region.ID, "title": region.Digest.Title},
		"chunks": nodes,
		"edges":  edges,
	})
}

func chunkLabel(content string) string {
	s := trimRunes(content, 48)
	return s
}

// atlasChunk renders a chunk-detail fragment for the network view's right pane.
func (h *handlers) atlasChunk(w http.ResponseWriter, r *http.Request) {
	c, err := h.d.Store.GetChunk(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "chunk not found", http.StatusNotFound)
		return
	}
	vm := viewmodel.Candidate{ChunkID: c.ID, SourceFile: c.SourceFile, Snippet: c.Content}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.AtlasChunkDetail(vm)); err != nil {
		log.Printf("atlas chunk: %v", err)
	}
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
