package web

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"log"
	"net/http"
	"sort"
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
	r.Get("/atlas/transcript", h.atlasTranscript)
	r.Get("/atlas/graph.json", h.atlasGraphJSON)
	r.Post("/atlas/rebuild", h.atlasRebuild)
}

func atlasSignals(st atlas.Status) map[string]any {
	return map[string]any{
		"atlasBuilding":   st.Building,
		"atlasRegions":    st.Regions,
		"atlasChunkCount": st.ChunkCount,
		"atlasError":      st.LastError,
		"atlasBuiltAt":    st.LastBuilt.UnixMilli(), // graph reloads itself when this changes
	}
}

// atlasPaneView assembles the #atlas fragment model from the service state.
func (h *handlers) atlasPaneView() viewmodel.AtlasPane {
	svc := h.d.Atlas
	st := svc.Status()
	vm := viewmodel.AtlasPane{Error: st.LastError, Building: st.Building, ChunkCount: st.ChunkCount}
	vm.ReadOnly = svc.ReadOnly
	vm.Holder = h.d.CorpusHolder

	build, _ := svc.Current()
	switch {
	case build != nil:
		// A finished build exists — keep the graph and region list usable
		// even while a rebuild runs behind it; the pure "building" takeover
		// is only for the very first build.
		vm.Status = "ready"
		vm.Rebuilding = st.Building
		vm.ChunkCount = build.ChunkCount
		for _, r := range build.Regions {
			vm.Regions = append(vm.Regions, viewmodel.AtlasRegionRow{
				ID: r.ID, Title: r.Digest.Title, Keywords: r.Digest.Keywords,
				ChunkCount: r.ChunkCount, Color: regionColor(r.ID),
			})
		}
		if cur, err := h.d.Corpus.ListChunkIDsWithEmbedding(); err == nil {
			if behind := len(cur) - build.ChunkCount; behind > 0 && behind*10 > build.ChunkCount {
				vm.Stale, vm.Behind = true, behind
			}
		}
	case st.Building:
		vm.Status = "building"
	case st.LastError != "":
		vm.Status = "failed"
	default:
		vm.Status = "none"
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
	// AtlasList only: this handler fires on every Atlas-tab open (and re-fires
	// on subsequent node focus changes), racing the same click's
	// window.atlasGraph.open() call. Re-patching #atlas-canvas here would
	// morph in a fresh, empty #atlas-graph div right as Cytoscape mounts its
	// canvas into the live one, wiping it out from under the user. The canvas
	// shell only needs to change on a real status transition (building ->
	// ready, etc.), which atlasRebuild patches separately.
	sse, err := Patch(w, r, components.AtlasList(vm))
	if err != nil {
		log.Printf("atlas pane: %v", err)
		return
	}
	// Keep the build-state signals fresh so the rebuilding banner's poll also
	// drives the graph's self-refresh once a rebuild lands.
	_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
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
				ID: reg.ID, Title: reg.Digest.Title, Keywords: reg.Digest.Keywords,
				ChunkCount: reg.ChunkCount, Color: regionColor(reg.ID),
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
	// A read-only session polls this endpoint; use it to notice a rebuild the
	// lock holder finished and swap the fresh build into memory.
	if h.d.Atlas != nil && h.d.Atlas.ReadOnly {
		if swapped, err := h.d.Atlas.Refresh(); err == nil && swapped {
			sse := datastar.NewSSE(w, r)
			vm := h.atlasPaneView()
			_ = sse.PatchElementTempl(components.AtlasList(vm))
			_ = sse.PatchElementTempl(components.AtlasCanvas(vm))
			_ = sse.MarshalAndPatchSignals(atlasSignals(h.d.Atlas.Status()))
			return
		}
	}
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

// --- network view (§6, thrice-revised): transcript graph JSON ---
//
// Top-level nodes are TRANSCRIPTS (source files) — a disjoint, already-
// meaningful partition, so no clustering is needed to produce it. Each is
// labelled from its own atlas.TranscriptDigest (built from that file's full
// text, not a region-based sample of chunks from elsewhere — see
// buildTranscriptDigests in internal/atlas/builder.go), falling back to the
// filename only if a transcript somehow has no digest. Edges are
// transcript-to-transcript, aggregated from a
// chunk-level similarity graph (atlas.BuildTranscriptEdges over
// atlas.BuildChunkEdges): the MAX chunk-chunk weight crossing between two
// files, not a sum or average. Edges are sent strongest-first and in full
// (a generous per-transcript K); the client renders only the top N of them,
// controlled by the "links" slider, re-running the layout on each change.
// Regions surface here only as each transcript node's fill colour (its
// dominant region, via atlas.TagFiles) and an informational `tags` list in
// the payload — not as hulls.
//
// There is no in-graph drill-down: tapping a transcript loads its chunk
// sequence into the right pane instead (see atlasTranscript).

type graphTag struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Keywords []string `json:"keywords"`
	Chunks   int      `json:"chunks"`
	Color    string   `json:"color"`
}

type graphTranscript struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Title    string   `json:"title"`
	Keywords []string `json:"keywords"`
	Chunks   int      `json:"chunks"`
	Tags     []string `json:"tags"`
	Color    string   `json:"color"` // primary region tag's colour
}

// regionPalette / regionColor map a region id to a stable display colour so
// the graph nodes and the region list agree without a shared legend lookup.
var regionPalette = []string{
	"#6c74ff", "#ff8c5a", "#4fc9a3", "#e6be55", "#c869c8", "#5fb8e6",
	"#e66e8c", "#96c85a", "#b58cff", "#e0a24f", "#5ad0b0", "#d9737d",
}

func regionColor(regionID string) string {
	if regionID == "" {
		return "#4a5578"
	}
	h := fnv.New32a()
	h.Write([]byte(regionID))
	return regionPalette[h.Sum32()%uint32(len(regionPalette))]
}

type graphEdge struct {
	A string  `json:"a"`
	B string  `json:"b"`
	W float64 `json:"w"`
}

// atlasGraphJSON serves the network-view payload. With no query it's the
// top-level transcript graph; with ?transcript=<id> it's that transcript's
// chunk sequence. The client lays it out with a force sim (top level) or an
// explicit sequential layout (drill-down) — no coordinates are sent (see
// source_atlas_plan.md §12).
func (h *handlers) atlasGraphJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	build, _ := h.d.Atlas.Current()
	if build == nil {
		json.NewEncoder(w).Encode(map[string]any{"tags": []any{}, "transcripts": []any{}, "edges": []any{}})
		return
	}

	ids := make([]string, 0, build.ChunkCount)
	tags := make([]graphTag, 0, len(build.Regions))
	for i := range build.Regions {
		reg := &build.Regions[i]
		tags = append(tags, graphTag{
			ID: reg.ID, Title: reg.Digest.Title, Keywords: reg.Digest.Keywords,
			Chunks: reg.ChunkCount, Color: regionColor(reg.ID),
		})
		for _, m := range reg.Members {
			ids = append(ids, m.ChunkID)
		}
	}

	chunks, _ := h.d.Corpus.GetChunksByIDs(ids)
	fileOf := make(map[string]string, len(chunks))  // chunk id -> source file
	fileChunks := make(map[string]int, len(chunks)) // source file -> chunk count
	vecByID := make(map[string][]float32, len(chunks))
	for i := range chunks {
		fileOf[chunks[i].ID] = chunks[i].SourceFile
		fileChunks[chunks[i].SourceFile]++
		if len(chunks[i].EmbeddingVec) > 0 {
			vecByID[chunks[i].ID] = ingest.BytesToFloat32s(chunks[i].EmbeddingVec)
		}
	}

	fileTags := atlas.TagFiles(build.Regions, fileOf, atlas.FileTagParams{})
	digestByFile := make(map[string]atlas.TranscriptDigest, len(build.Transcripts))
	for _, td := range build.Transcripts {
		digestByFile[td.SourceFile] = td
	}

	vecs := make([][]float32, len(ids))
	for i, id := range ids {
		vecs[i] = vecByID[id]
	}
	// atlas.BuildChunkEdges' output is never sent to the client here — it's
	// only the substrate BuildTranscriptEdges aggregates up to file weights.
	// No KeepTopFraction trim and generous Ks: the "clump" problem is now the
	// client's "links" slider to solve (it draws only the strongest N), so we
	// hand it the full candidate set to slide through, not a pre-thinned one.
	chunkEdges := atlas.BuildChunkEdges(ids, vecs, atlas.GraphEdgeParams{K: 10, MinWeight: 0.3})
	transcriptEdges := atlas.BuildTranscriptEdges(chunkEdges, fileOf, atlas.TranscriptEdgeParams{K: 12})

	files := make([]string, 0, len(fileChunks))
	for f := range fileChunks {
		files = append(files, f)
	}
	sort.Strings(files)

	transcripts := make([]graphTranscript, 0, len(files))
	for _, f := range files {
		tagIDs := make([]string, 0, len(fileTags[f]))
		for _, t := range fileTags[f] {
			tagIDs = append(tagIDs, t.RegionID)
		}
		var primary string
		if len(tagIDs) > 0 {
			primary = tagIDs[0]
		}
		d := digestByFile[f].Digest
		transcripts = append(transcripts, graphTranscript{
			ID: f, Label: transcriptLabel(d, f), Title: d.Title, Keywords: d.Keywords,
			Chunks: fileChunks[f], Tags: tagIDs, Color: regionColor(primary),
		})
	}

	edges := make([]graphEdge, 0, len(transcriptEdges))
	for _, e := range transcriptEdges {
		edges = append(edges, graphEdge{A: e.A, B: e.B, W: e.Weight})
	}

	json.NewEncoder(w).Encode(map[string]any{
		"tags": tags, "transcripts": transcripts, "edges": edges,
		"buildAt": h.d.Atlas.Status().LastBuilt.UnixMilli(),
	})
}

// transcriptLabel prefers the digest's keywords (top 3), falls back to its
// title, and falls back to the filename when there's no digest at all yet.
func transcriptLabel(d atlas.Digest, filename string) string {
	if len(d.Keywords) > 0 {
		kw := d.Keywords
		if len(kw) > 3 {
			kw = kw[:3]
		}
		return strings.Join(kw, " · ")
	}
	if d.Title != "" {
		return d.Title
	}
	return filename
}

// atlasTranscript renders one source file's chunks, in document order, into
// the Atlas right pane (#atlas-transcript, AtlasTranscript): each chunk as its
// short summary label (atlas_chunk_label — the same phrase the region member
// list shows), expandable in place to the passage text, with the shared
// read-in-transcript / attach actions. This is the linear read of a
// transcript, replacing the old in-graph chunk drill-down.
//
//	GET /atlas/transcript?file=<source file>
func (h *handlers) atlasTranscript(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	chunks, err := h.d.Corpus.ListChunksBySourceFile(file)
	if err != nil || len(chunks) == 0 {
		http.Error(w, "transcript not found", http.StatusNotFound)
		return
	}

	ids := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
	}
	labels := h.d.Atlas.ChunkLabels(ids)

	vm := viewmodel.AtlasTranscriptDetail{File: file}
	if build, _ := h.d.Atlas.Current(); build != nil {
		for _, td := range build.Transcripts {
			if td.SourceFile == file {
				vm.Keywords = td.Digest.Keywords
				break
			}
		}
	}
	for _, c := range chunks {
		summary := labels[c.ID]
		if summary == "" {
			summary = trimRunes(c.Content, 60) // no persisted label yet (LLM off, or not rebuilt)
		}
		vm.Members = append(vm.Members, viewmodel.Candidate{
			ChunkID:    c.ID,
			SourceFile: c.SourceFile,
			Snippet:    trimRunes(c.Content, snippetRunes),
			Summary:    summary,
		})
	}

	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.AtlasTranscript(vm)); err != nil {
		log.Printf("atlas transcript: %v", err)
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
	memberIDs := make([]string, 0, len(region.Members))
	for _, m := range region.Members {
		memberIDs = append(memberIDs, m.ChunkID)
	}
	labels := h.d.Atlas.ChunkLabels(memberIDs) // chunk id -> short LLM label; same as the graph drill-down
	for _, m := range region.Members {
		c, err := h.d.Corpus.GetChunk(m.ChunkID)
		if err != nil {
			continue
		}
		vm.Members = append(vm.Members, viewmodel.Candidate{
			ChunkID:    c.ID,
			SourceFile: c.SourceFile,
			Snippet:    trimRunes(c.Content, snippetRunes),
			Summary:    labels[c.ID],
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
		vm.Neighbours = append(vm.Neighbours, viewmodel.AtlasRegionRow{ID: other, Title: titleByID[other], Color: regionColor(other)})
	}

	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(components.AtlasRegion(vm)); err != nil {
		log.Printf("atlas region: %v", err)
	}
}
