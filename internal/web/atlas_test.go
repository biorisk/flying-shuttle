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
	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/storetest"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func atlasTestServer(t *testing.T) (doc.Store, corpus.Store, *atlas.Service, http.Handler) {
	t.Helper()
	sp := storetest.New(t)
	s := sp.Doc

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
	if err := sp.Corpus.CreateChunks(chunks); err != nil {
		t.Fatal(err)
	}

	emb := &ingest.StubEmbedder{Dim: 8}
	svc := &atlas.Service{Embedder: emb, Builder: &atlas.Builder{
		Store:    atlas.NewStore(sp.Corpus.DB()),
		Corpus:   func() ([]atlas.CorpusChunk, error) { return corpus, nil },
		Embedder: emb,
		Labeller: &atlas.ChunkLabeller{Complete: stubCompleter{}, ModelName: "stub"},
		Params:   atlas.BuildParams{MinChunks: 6},
	}}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Corpus: sp.Corpus, Atlas: svc})
	return s, sp.Corpus, svc, r
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
	_, _, svc, r := atlasTestServer(t)

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
	_, _, svc, r := atlasTestServer(t)
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
	_, _, svc, r := atlasTestServer(t)
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
	// Members show the chunk's LLM summary label; clicking it reveals the text.
	if !strings.Contains(body, "atlas-member-quote") || !strings.Contains(body, "atlas-member-summary") {
		t.Fatalf("member summary disclosure missing: %s", body)
	}
	if !strings.Contains(body, "passage label") {
		t.Fatalf("stub chunk label not rendered as summary: %s", body)
	}
	// "Read in transcript" must close the atlas pane so the reader (which lives
	// in #evidence, behind the atlas pane) becomes visible.
	if !strings.Contains(body, "$atlasOpen = false") {
		t.Fatalf("read-in-transcript does not close the atlas pane: %s", body)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/regions/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown region: %d", rec.Code)
	}
}

func TestAtlasSearch_RanksRegions(t *testing.T) {
	_, _, svc, r := atlasTestServer(t)
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
	_, _, svc, r := atlasTestServer(t)
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

func TestAtlasTranscriptPane(t *testing.T) {
	_, _, svc, r := atlasTestServer(t)
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/transcript?file=sailing.txt", nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("transcript pane: %d %s", rec.Code, body)
	}
	// Renders into #atlas-transcript, gated on $atlasTranscriptId, as the
	// shared passage list: summary label + expand-in-place text + read/attach.
	for _, want := range []string{
		`id="atlas-transcript"`, "$atlasTranscriptId === ", "atlas-members",
		"atlas-member-summary", "Read in transcript", "/evidence?chunk_id=",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in transcript pane: %s", want, body)
		}
	}
	// One row per chunk of sailing.txt (8), each an expand-in-place quote whose
	// summary is the persisted (stub) LLM label, never a raw text head.
	if n := strings.Count(body, "atlas-member-quote"); n != 8 {
		t.Fatalf("want 8 passages for sailing.txt, got %d", n)
	}
	if !strings.Contains(body, "passage label ") {
		t.Fatalf("summary is not the persisted LLM label: %s", body)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/atlas/transcript?file=nope.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown transcript: %d", rec.Code)
	}
}

