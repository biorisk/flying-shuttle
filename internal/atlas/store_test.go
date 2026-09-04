package atlas_test

import (
	"errors"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
)

func newTestStore(t *testing.T) (*store.SQLiteStore, atlas.Store) {
	t.Helper()
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s, atlas.NewStore(s.DB())
}

func seedChunks(t *testing.T, s *store.SQLiteStore, ids ...string) {
	t.Helper()
	chunks := make([]model.Chunk, len(ids))
	for i, id := range ids {
		chunks[i] = model.Chunk{ID: id, SourceFile: "t.txt", Content: "content " + id}
	}
	if err := s.CreateChunks(chunks); err != nil {
		t.Fatalf("seed chunks: %v", err)
	}
}

func TestBuildRoundTrip(t *testing.T) {
	s, as := newTestStore(t)
	seedChunks(t, s, "c1", "c2", "c3", "c4")

	b := &atlas.Build{ChunkCount: 4, Params: `{"k":2}`}
	if err := as.CreateBuild(b); err != nil {
		t.Fatalf("CreateBuild: %v", err)
	}
	if b.ID == "" || b.Status != atlas.StatusBuilding {
		t.Fatalf("CreateBuild did not fill id/status: %+v", b)
	}

	regions := []atlas.Region{
		{
			Centroid:   []float32{1, 0, 0},
			ChunkCount: 2,
			Digest:     atlas.Digest{Title: "Region one", Keywords: []string{"alpha", "beta"}, Source: "extractive"},
			Members: []atlas.Member{
				{ChunkID: "c2", Distance: 0.4, Keywords: []string{"beta"}},
				{ChunkID: "c1", Distance: 0.1, Keywords: []string{"alpha"}},
			},
		},
		{
			Centroid:   []float32{0, 1, 0},
			ChunkCount: 2,
			Digest:     atlas.Digest{Title: "Region two"},
			Members: []atlas.Member{
				{ChunkID: "c3", Distance: 0.2},
				{ChunkID: "c4", Distance: 0.3},
			},
		},
	}
	if err := as.InsertRegions(b.ID, regions); err != nil {
		t.Fatalf("InsertRegions: %v", err)
	}
	if regions[0].ID == "" || regions[1].ID == "" {
		t.Fatal("InsertRegions did not fill region ids")
	}

	link, ok := atlas.NewLink(regions[0].ID, regions[1].ID, 0.73)
	if !ok {
		t.Fatal("NewLink returned !ok for distinct regions")
	}
	if err := as.InsertLinks(b.ID, []atlas.Link{link}); err != nil {
		t.Fatalf("InsertLinks: %v", err)
	}

	if err := as.SetRegionDigestVec(regions[0].ID, []float32{0.5, 0.5, 0.5}); err != nil {
		t.Fatalf("SetRegionDigestVec: %v", err)
	}
	if err := as.SetBuildStatus(b.ID, atlas.StatusReady, 4, ""); err != nil {
		t.Fatalf("SetBuildStatus: %v", err)
	}

	got, err := as.CurrentBuild()
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	if got.Status != atlas.StatusReady || len(got.Regions) != 2 || len(got.Links) != 1 {
		t.Fatalf("unexpected build: status=%s regions=%d links=%d", got.Status, len(got.Regions), len(got.Links))
	}

	// Regions are ordered by chunk_count desc then id; both have 2 chunks, so
	// find region one by title.
	var r1 *atlas.Region
	for i := range got.Regions {
		if got.Regions[i].Digest.Title == "Region one" {
			r1 = &got.Regions[i]
		}
	}
	if r1 == nil {
		t.Fatal("region one not found")
	}
	if len(r1.Digest.Keywords) != 2 || r1.Digest.Keywords[0] != "alpha" {
		t.Fatalf("keywords round-trip: %v", r1.Digest.Keywords)
	}
	if len(r1.DigestVec) != 3 || r1.DigestVec[0] != 0.5 {
		t.Fatalf("digest vec round-trip: %v", r1.DigestVec)
	}
	if len(r1.Members) != 2 || r1.Members[0].ChunkID != "c1" {
		t.Fatalf("members not ordered by distance: %+v", r1.Members)
	}
	if got.Links[0].Weight != 0.73 || got.Links[0].RegionA >= got.Links[0].RegionB {
		t.Fatalf("link round-trip / ordering: %+v", got.Links[0])
	}
}

func TestPruneExceptAndNoBuild(t *testing.T) {
	s, as := newTestStore(t)
	seedChunks(t, s, "c1")

	if _, err := as.CurrentBuild(); !errors.Is(err, atlas.ErrNoBuild) {
		t.Fatalf("expected ErrNoBuild, got %v", err)
	}

	var keep string
	for i := 0; i < 3; i++ {
		b := &atlas.Build{}
		if err := as.CreateBuild(b); err != nil {
			t.Fatalf("CreateBuild: %v", err)
		}
		if err := as.InsertRegions(b.ID, []atlas.Region{{
			Centroid: []float32{1}, ChunkCount: 1,
			Members: []atlas.Member{{ChunkID: "c1"}},
		}}); err != nil {
			t.Fatalf("InsertRegions: %v", err)
		}
		keep = b.ID
	}

	if err := as.PruneExcept(keep); err != nil {
		t.Fatalf("PruneExcept: %v", err)
	}
	builds, err := as.ListBuilds()
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	if len(builds) != 1 || builds[0].ID != keep {
		t.Fatalf("PruneExcept left %d builds", len(builds))
	}

	// Cascade: the pruned builds' regions and memberships are gone.
	b, err := as.GetBuild(keep)
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if len(b.Regions) != 1 || len(b.Regions[0].Members) != 1 {
		t.Fatalf("kept build body wrong: %+v", b.Regions)
	}
}
