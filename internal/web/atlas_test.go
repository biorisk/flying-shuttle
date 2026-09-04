package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func atlasTestServer(t *testing.T) (*store.SQLiteStore, *atlas.Service, http.Handler) {
	t.Helper()
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 3 topical blobs, deterministic axis-aligned vectors.
	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	bodies := []string{
		"the mainsail luffed as the skipper tacked the sloop through the wind",
		"she folded cold butter into the flour until the pastry dough held",
		"the borrow checker rejected the mutable reference that outlived its scope",
	}
	for g := 0; g < 3; g++ {
		for i := 0; i < 8; i++ {
			id := string(rune('A'+g)) + string(rune('0'+i))
			v := make([]float32, 6)
			v[g] = 1
			body := bodies[g]
			chunks = append(chunks, model.Chunk{ID: id, SourceFile: "t.txt", Content: body, EmbeddingVec: ingest.Float32sToBytes(v)})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: v})
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
		Params:   atlas.BuildParams{MinChunks: 6},
	}}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Atlas: svc})
	return s, svc, r
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

func TestAtlasGraphJSON(t *testing.T) {
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
		Regions []struct {
			ID       string   `json:"id"`
			Keywords []string `json:"keywords"`
			Chunks   int      `json:"chunks"`
		} `json:"regions"`
		Links []struct{ A, B string } `json:"links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if len(g.Regions) != len(build.Regions) {
		t.Fatalf("want %d regions, got %d", len(build.Regions), len(g.Regions))
	}

	// Region drill-down: member chunks + intra-region edges.
	rid := build.Regions[0].ID
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/graph.json?region="+rid, nil))
	var rg struct {
		Region map[string]string `json:"region"`
		Chunks []struct{ ID, Label string } `json:"chunks"`
		Edges  []struct {
			A, B string
			W    float64
		} `json:"edges"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rg); err != nil {
		t.Fatalf("region graph decode: %v — %s", err, rec.Body.String())
	}
	if rg.Region["id"] != rid || len(rg.Chunks) != build.Regions[0].ChunkCount {
		t.Fatalf("region graph: %+v", rg)
	}
	// The blob members are near-identical, so every pair should be linked.
	if len(rg.Edges) == 0 {
		t.Fatalf("expected intra-region edges for a tight blob")
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
	if rec.Code != 200 || !strings.Contains(body, "atlas-detail-text") || !strings.Contains(body, "/evidence?chunk_id=") {
		t.Fatalf("chunk detail: %d %s", rec.Code, body)
	}
}
