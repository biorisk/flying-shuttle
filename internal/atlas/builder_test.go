package atlas_test

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
)

// buildCorpus makes N chunks split across `groups` topics, each with a
// deterministic embedding pointing along one axis, and persists them.
func buildCorpus(t *testing.T, s *store.SQLiteStore, groups, per, dim int) func() ([]atlas.CorpusChunk, error) {
	t.Helper()
	var chunks []model.Chunk
	var corpus []atlas.CorpusChunk
	for g := 0; g < groups; g++ {
		for i := 0; i < per; i++ {
			id := string(rune('A'+g)) + string(rune('0'+i%10)) + string(rune('a'+i/10))
			vec := make([]float32, dim)
			vec[g%dim] = 1
			body := topics[g%len(topics)]
			chunks = append(chunks, model.Chunk{
				ID: id, SourceFile: "t.txt", Content: body,
				EmbeddingVec: ingest.Float32sToBytes(vec),
			})
			corpus = append(corpus, atlas.CorpusChunk{ID: id, Content: body, Vec: vec})
		}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return func() ([]atlas.CorpusChunk, error) { return corpus, nil }
}

var topics = []string{
	"The mainsail luffed as the skipper tacked the sloop hard through the wind.",
	"She folded cold butter into the flour until the pastry dough just held together.",
	"The compiler rejected the borrow because the mutable reference outlived its scope.",
}

func TestBuilder_Build(t *testing.T) {
	s, as := newTestStore(t)
	corpus := buildCorpus(t, s, 3, 14, 6)

	b := &atlas.Builder{
		Store:      as,
		Corpus:     corpus,
		Embedder:   &ingest.StubEmbedder{Dim: 16},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 6},
	}

	build, idx, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if build.Status != atlas.StatusReady {
		t.Fatalf("status = %s", build.Status)
	}
	if len(build.Regions) < 3 {
		t.Fatalf("want >=3 regions, got %d", len(build.Regions))
	}
	if build.ChunkCount != 42 {
		t.Fatalf("chunk count = %d", build.ChunkCount)
	}

	total := 0
	for _, r := range build.Regions {
		total += len(r.Members)
		if r.Digest.Title == "" || len(r.Digest.Keywords) == 0 {
			t.Fatalf("region digest not filled: %+v", r.Digest)
		}
		if r.Digest.Source != "extractive" {
			t.Fatalf("digest source = %q", r.Digest.Source)
		}
		for _, m := range r.Members {
			if len(m.Keywords) == 0 {
				t.Fatalf("member %s missing keywords", m.ChunkID)
			}
		}
		if len(r.DigestVec) != 16 {
			t.Fatalf("digest vec not embedded: %d", len(r.DigestVec))
		}
	}
	if total != 42 {
		t.Fatalf("partition lost chunks: %d", total)
	}
	if idx.Len() != len(build.Regions) {
		t.Fatalf("region index size %d != regions %d", idx.Len(), len(build.Regions))
	}

	// Persisted as the current build.
	got, err := as.CurrentBuild()
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	if got.ID != build.ID || len(got.Regions) != len(build.Regions) {
		t.Fatalf("current build mismatch")
	}

	// A second build prunes the first.
	if _, _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	builds, _ := as.ListBuilds()
	if len(builds) != 1 {
		t.Fatalf("old build not pruned: %d remain", len(builds))
	}
}

func TestBuilder_TooFewChunks(t *testing.T) {
	s, as := newTestStore(t)
	corpus := buildCorpus(t, s, 1, 3, 4)
	b := &atlas.Builder{
		Store: as, Corpus: corpus,
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 10},
	}
	if _, _, err := b.Build(context.Background()); err == nil {
		t.Fatal("expected ErrTooFewChunks")
	}
	builds, _ := as.ListBuilds()
	if len(builds) != 0 {
		t.Fatalf("failed precondition should not leave a build row: %d", len(builds))
	}
}

func TestService_RebuildSingleFlight(t *testing.T) {
	s, as := newTestStore(t)
	corpus := buildCorpus(t, s, 2, 12, 5)
	svc := &atlas.Service{Builder: &atlas.Builder{
		Store: as, Corpus: corpus, Embedder: &ingest.StubEmbedder{Dim: 8},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 6},
	}}

	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	st := svc.Status()
	if st.Building || st.Regions == 0 || st.LastError != "" {
		t.Fatalf("post-build status: %+v", st)
	}
	cur, idx := svc.Current()
	if cur == nil || idx == nil {
		t.Fatal("Current() nil after build")
	}

	// LoadCurrent on a fresh service recovers the same build.
	svc2 := &atlas.Service{Builder: svc.Builder}
	if err := svc2.LoadCurrent(); err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if c2, _ := svc2.Current(); c2 == nil || c2.ID != cur.ID {
		t.Fatal("LoadCurrent did not restore the build")
	}
}
