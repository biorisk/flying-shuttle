package atlas_test

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// TestService_ReadOnlyRefusesRebuildAndRefreshes covers the Phase 3 read-only
// corpus session: StartRebuild / Rebuild are refused, but Refresh picks up a
// build another (writer) session persisted.
func TestService_ReadOnlyRefusesRebuildAndRefreshes(t *testing.T) {
	s, as := newTestStore(t)

	// A writer service builds and persists.
	writer := &atlas.Service{Builder: &atlas.Builder{
		Store:      as,
		Corpus:     buildCorpus(t, s, 3, 14, 6),
		Embedder:   &ingest.StubEmbedder{Dim: 16},
		Summariser: &atlas.ExtractiveSummariser{KW: atlas.NewKeyworder(topics)},
		Params:     atlas.BuildParams{MinChunks: 6},
	}}
	if err := writer.Rebuild(context.Background()); err != nil {
		t.Fatalf("writer build: %v", err)
	}

	// A read-only service over the same store starts with nothing loaded.
	ro := &atlas.Service{ReadOnly: true, Builder: &atlas.Builder{Store: as}}
	if b, _ := ro.Current(); b != nil {
		t.Fatal("read-only service should start empty")
	}
	if ro.StartRebuild() {
		t.Fatal("StartRebuild must be refused on a read-only service")
	}
	if err := ro.Rebuild(context.Background()); err != atlas.ErrReadOnly {
		t.Fatalf("Rebuild err = %v, want ErrReadOnly", err)
	}

	// Refresh pulls in the writer's build.
	swapped, err := ro.Refresh()
	if err != nil || !swapped {
		t.Fatalf("Refresh: swapped=%v err=%v", swapped, err)
	}
	if b, _ := ro.Current(); b == nil {
		t.Fatal("Refresh did not load the current build")
	}
	// A second Refresh with no change is a no-op.
	if swapped, _ := ro.Refresh(); swapped {
		t.Fatal("Refresh should be a no-op when the build id is unchanged")
	}
}
