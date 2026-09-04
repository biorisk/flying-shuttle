package atlas

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

func TestRegionIndex_Rank(t *testing.T) {
	ix := NewRegionIndex()
	ix.Add("sail", []float32{1, 0, 0})
	ix.Add("bake", []float32{0, 1, 0})
	ix.Add("code", []float32{0, 0, 1})

	hits := ix.Rank([]float32{0.9, 0.1, 0}, 2)
	if len(hits) != 2 || hits[0].RegionID != "sail" {
		t.Fatalf("expected sail first: %+v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("not sorted by score: %+v", hits)
	}

	if ix.Rank(nil, 5) != nil {
		t.Fatal("empty query should yield nil")
	}
	if NewRegionIndex().Rank([]float32{1}, 5) != nil {
		t.Fatal("empty index should yield nil")
	}
}

func TestEmbedDigests_StubAndLoad(t *testing.T) {
	regions := []Region{
		{ID: "r1", Digest: Digest{Title: "Sailing upwind", Abstract: "Tacking and trim."}},
		{ID: "r2", Digest: Digest{Title: "Cold proof", Keywords: []string{"sourdough", "crumb"}}},
		{ID: "r3"}, // empty digest — skipped
	}
	emb := &ingest.StubEmbedder{Dim: 16}
	if err := EmbedDigests(context.Background(), emb, regions); err != nil {
		t.Fatalf("EmbedDigests: %v", err)
	}
	if len(regions[0].DigestVec) != 16 || len(regions[1].DigestVec) != 16 {
		t.Fatalf("digests not embedded: %d %d", len(regions[0].DigestVec), len(regions[1].DigestVec))
	}
	if regions[2].DigestVec != nil {
		t.Fatal("empty-digest region should not be embedded")
	}

	b := &Build{Regions: regions}
	ix := LoadRegionIndex(b)
	if ix.Len() != 2 {
		t.Fatalf("index should hold the 2 embedded regions, got %d", ix.Len())
	}
}

func TestEmbedDigests_NilEmbedder(t *testing.T) {
	err := EmbedDigests(context.Background(), nil, []Region{{Digest: Digest{Title: "x"}}})
	if err != ingest.ErrEmbedderNotReady {
		t.Fatalf("want ErrEmbedderNotReady, got %v", err)
	}
}
