package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func atlasTestServer(t *testing.T) (*doc.SQLiteStore, *atlas.Service, http.Handler) {
	t.Helper()
	s, _ := doc.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 3 topical blobs, each its own source file (transcript), deterministic
	// axis-aligned vectors, StartOffset set so drill-down order is exact.
	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	bodies := []string{
		"the mainsail luffed as the skipper tacked the sloop through the wind",
		"she folded cold butter into the flour until the pastry dough held",
		"the borrow checker rejected the mutable reference that outlived its scope",
	}
	files := []string{"sailing.txt", "baking.txt", "rust.txt"}
	for g := 0; g < 3; g++ {
		for i := 0; i < 8; i++ {
			id := string(rune('A'+g)) + string(rune('0'+i))
			v := make([]float32, 6)
			v[g] = 1
			body := bodies[g]
			chunks = append(chunks, model.Chunk{ID: id, SourceFile: files[g], Content: body, StartOffset: i, EmbeddingVec: ingest.Float32sToBytes(v)})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: v, SourceFile: files[g], StartOffset: i})
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatal(err)
	}

	emb := &ingest.StubEmbedder{Dim: 8}
	svc := &atlas.Service{Embedder: emb, Builder: &atlas.Builder{
		Store:    atlas.NewStore(s.DB()),
		Corpus:   func() ([]atlas.CorpusChunk, error) { return corpus, nil },
		Embedder: emb,
		Labeller: &atlas.ChunkLabeller{Complete: stubCompleter{}, ModelName: "stub"},
		Params:   atlas.BuildParams{MinChunks: 6},
	}}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Atlas: svc})
	return s, svc, r
}

// stubCompleter echoes one "<n>. passage label n" line per numbered passage
// in the prompt, so ChunkLabeller produces deterministic non-snippet labels.
type stubCompleter struct{}

func (stubCompleter) Complete(_ context.Context, _, userPrompt string) (string, error) {
	n := strings.Count(userPrompt, "\n\n") // one blank line after each passage
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%d. passage label %d\n", i, i)
	}
	return b.String(), nil
}

func TestAtlasPane_EmptyThenReady(t *testing.T) {
	_, svc, r := atlasTestServer(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Build the atlas") {
		t.Fatalf("idle pane: %d %s", rec.Code, rec.Body.String())
	}

	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "regions ·") {
		t.Fatalf("ready pane: %d %s", rec.Code, body)
	}
	if !strings.Contains(body, "atlas-region-head") {
		t.Fatalf("no region rows rendered: %s", body)
	}
}

func TestAtlasPane_GraphStaysUsableDuringRebuild(t *testing.T) {
	_, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Hold the next rebuild open in its first phase.
	block := make(chan struct{})
	orig := svc.Builder.Corpus
	svc.Builder.Corpus = func() ([]atlas.CorpusChunk, error) { <-block; return orig() }
	if !svc.StartRebuild() {
		t.Fatal("StartRebuild returned false")
	}
	for i := 0; i < 500 && !svc.Status().Building; i++ {
		time.Sleep(time.Millisecond)
	}
	if !svc.Status().Building {
		close(block)
		t.Fatal("service never entered the building state")
	}

	// The pane still shows the previous build's region list, plus a banner —
	// not the first-build "Building the atlas…" takeover.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "atlas-region-head") {
		t.Fatalf("region list disappeared during rebuild: %s", body)
	}
	if !strings.Contains(body, "Rebuilding") || strings.Contains(body, "Building the atlas") {
		t.Fatalf("expected the rebuilding banner, not the first-build takeover: %s", body)
	}

	// And the graph endpoint still serves the prior build.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/graph.json", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"transcripts"`) {
		t.Fatalf("graph.json unavailable during rebuild: %d %s", rec.Code, rec.Body.String())
	}

	close(block)
	for i := 0; i < 2000 && svc.Status().Building; i++ {
		time.Sleep(time.Millisecond)
	}
}

func TestAtlasRegion_DetailAndMembers(t *testing.T) {
	_, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	build, _ := svc.Current()
	if len(build.Regions) == 0 {
		t.Fatal("no regions")
	}
	rid := build.Regions[0].ID

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/regions/"+rid, nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("region detail: %d %s", rec.Code, body)
	}
	if !strings.Contains(body, "atlas-members") || !strings.Contains(body, "Add as evidence") {
		t.Fatalf("member cards missing: %s", body)
	}
	if !strings.Contains(body, "/evidence?chunk_id=") {
		t.Fatalf("attach wiring missing: %s", body)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/regions/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown region: %d", rec.Code)
	}
}

func TestAtlasSearch_RanksRegions(t *testing.T) {
	_, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/search?q=sailing+upwind", nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("search: %d %s", rec.Code, body)
	}
	// StubEmbedder is deterministic-random, so we can't assert *which* region
	// ranks first — only that the fragment renders hop buttons.
	if !strings.Contains(body, `id="atlas-matches"`) {
		t.Fatalf("no matches fragment: %s", body)
	}
	if !strings.Contains(body, "atlas-hop") {
		t.Fatalf("no ranked regions rendered: %s", body)
	}

	// Empty query -> empty (but valid) fragment.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/search?q=", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "atlas-hop") {
		t.Fatalf("empty query should yield an empty fragment: %s", rec.Body.String())
	}
}

func TestAtlasGraphJSON_TopLevelIsTranscripts(t *testing.T) {
	_, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	build, _ := svc.Current()

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/graph.json", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("graph.json: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var g struct {
		Tags []struct {
			ID       string   `json:"id"`
			Keywords []string `json:"keywords"`
			Chunks   int      `json:"chunks"`
		} `json:"tags"`
		Transcripts []struct {
			ID       string   `json:"id"`
			Label    string   `json:"label"`
			Keywords []string `json:"keywords"`
			Chunks   int      `json:"chunks"`
			Tags     []string `json:"tags"`
			Color    string   `json:"color"`
		} `json:"transcripts"`
		Edges []struct {
			A, B string
			W    float64
		} `json:"edges"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if len(g.Tags) != len(build.Regions) {
		t.Fatalf("want %d tags, got %d", len(build.Regions), len(g.Tags))
	}
	// Three source files, not build.ChunkCount chunk nodes.
	if len(g.Transcripts) != 3 {
		t.Fatalf("want 3 transcript nodes, got %d: %+v", len(g.Transcripts), g.Transcripts)
	}
	total := 0
	for _, tr := range g.Transcripts {
		total += tr.Chunks
		if len(tr.Tags) == 0 {
			t.Fatalf("transcript %s has no region tag", tr.ID)
		}
		// Labelled from its own digest (keywords), not the bare filename.
		if len(tr.Keywords) == 0 {
			t.Fatalf("transcript %s has no digest keywords", tr.ID)
		}
		if tr.Color == "" || tr.Color[0] != '#' {
			t.Fatalf("transcript %s has no region colour: %q", tr.ID, tr.Color)
		}
		if tr.Label == tr.ID {
			t.Fatalf("transcript %s label fell back to the filename: %+v", tr.ID, tr)
		}
	}
	if total != build.ChunkCount {
		t.Fatalf("transcript chunk counts sum to %d, want %d", total, build.ChunkCount)
	}
	// The three blobs are orthogonal (no cross-file embedding similarity), so
	// no transcript-transcript edges are expected — just confirming the field
	// decodes and isn't required to be non-empty.
	_ = g.Edges
}

func TestAtlasGraphJSON_TranscriptDrillDown(t *testing.T) {
	_, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/graph.json?transcript=sailing.txt", nil))
	if rec.Code != 200 {
		t.Fatalf("transcript drill-down: %d %s", rec.Code, rec.Body.String())
	}
	var g struct {
		Transcript map[string]any                              `json:"transcript"`
		Chunks     []struct{ ID, Label, Region, Color string } `json:"chunks"`
		Edges      []struct{ A, B string }                     `json:"edges"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if len(g.Chunks) != 8 {
		t.Fatalf("want 8 chunks for sailing.txt, got %d", len(g.Chunks))
	}
	// Labels come from the (stub) LLM labeller and are persisted per chunk,
	// never the old text-head snippet; each chunk carries its region + colour.
	for _, c := range g.Chunks {
		if !strings.HasPrefix(c.Label, "passage label ") {
			t.Fatalf("chunk %s label is not the persisted LLM label: %q", c.ID, c.Label)
		}
		if c.Region == "" || c.Color == "" || c.Color[0] != '#' {
			t.Fatalf("chunk %s missing region/colour: region=%q color=%q", c.ID, c.Region, c.Color)
		}
	}
	// Strictly sequential: 8 chunks -> 7 adjacency edges, chunk[i]-chunk[i+1].
	if len(g.Edges) != 7 {
		t.Fatalf("want 7 sequential edges, got %d: %+v", len(g.Edges), g.Edges)
	}
	for i, e := range g.Edges {
		if e.A != g.Chunks[i].ID || e.B != g.Chunks[i+1].ID {
			t.Fatalf("edge %d = %+v, want %s-%s (document order)", i, e, g.Chunks[i].ID, g.Chunks[i+1].ID)
		}
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/graph.json?transcript=nope.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown transcript: %d", rec.Code)
	}
}

func TestAtlasChunkDetail(t *testing.T) {
	_, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	build, _ := svc.Current()
	cid := build.Regions[0].Members[0].ChunkID

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/chunk/"+cid, nil))
	body := rec.Body.String()
	// Renders into the right pane (#atlas-selected-chunk) as an evidence-style
	// candidate card, gated on $atlasChunkId, with the shared attach action.
	if rec.Code != 200 {
		t.Fatalf("chunk detail: %d %s", rec.Code, body)
	}
	for _, want := range []string{`id="atlas-selected-chunk"`, "$atlasChunkId === ", "candidate-text", "/evidence?chunk_id="} {
		if !strings.Contains(body, want) {
			t.Fatalf("chunk detail missing %q: %s", want, body)
		}
	}
}
