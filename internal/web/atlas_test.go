package web_test

import (
	"context"
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
