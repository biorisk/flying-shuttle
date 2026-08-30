package indexer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/google/uuid"
)

// fakeEmbedder returns deterministic 4-dim vectors and can pretend to be down.
type fakeEmbedder struct{ down bool }

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	v, err := f.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	if f.down {
		return nil, ingest.ErrEmbedderNotReady
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		var s float32
		for _, r := range t {
			s += float32(r)
		}
		out[i] = []float32{s, float32(len(t)), 1, 0}
	}
	return out, nil
}

func newStore(t *testing.T) store.Store {
	t.Helper()
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkChunk(t *testing.T, s store.Store, content string) model.Chunk {
	t.Helper()
	c := model.Chunk{ID: uuid.NewString(), SourceFile: "f.txt", Content: content, EndOffset: len(content)}
	if err := s.CreateChunk(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBackfiller_embedsMissingChunks(t *testing.T) {
	s := newStore(t)
	idx := search.NewHybridIndex(nil)
	c1 := mkChunk(t, s, "hello world")
	c2 := mkChunk(t, s, "another chunk of text")
	idx.IndexChunk(&c1)
	idx.IndexChunk(&c2)

	bf := NewBackfiller(s, &fakeEmbedder{}, idx, 16, time.Hour)
	bf.drain(context.Background())

	n, err := s.CountChunksMissingEmbedding()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d chunks still missing embeddings", n)
	}
	if !idx.Vector.Has(c1.ID) || !idx.Vector.Has(c2.ID) {
		t.Fatal("vector index not populated")
	}

	got, err := s.GetChunk(c1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EmbeddingVec) == 0 {
		t.Fatal("embedding not persisted to store")
	}
}

func TestBackfiller_pausesWhenEmbedderDown(t *testing.T) {
	s := newStore(t)
	idx := search.NewHybridIndex(nil)
	mkChunk(t, s, "will not be embedded yet")

	bf := NewBackfiller(s, &fakeEmbedder{down: true}, idx, 16, time.Hour)
	bf.drain(context.Background())

	n, _ := s.CountChunksMissingEmbedding()
	if n != 1 {
		t.Fatalf("expected 1 missing, got %d", n)
	}
}

func TestSnapshotter_flushWritesFiles(t *testing.T) {
	dir := t.TempDir()
	bm25Path := filepath.Join(dir, "x.bm25")
	hnswPath := filepath.Join(dir, "x.hnsw")

	idx := search.NewHybridIndex(nil)
	idx.IndexChunk(&model.Chunk{ID: "c1", Content: "some searchable content"})

	sn := NewSnapshotter(idx, bm25Path, hnswPath, time.Hour)
	if err := sn.Flush(); err != nil {
		t.Fatal(err)
	}
	if idx.Dirty() {
		t.Fatal("index still dirty after flush")
	}

	reloaded := search.NewHybridIndex(nil)
	if ok, err := reloaded.LoadBM25(bm25Path); err != nil || !ok {
		t.Fatalf("reload: ok=%v err=%v", ok, err)
	}
	if !reloaded.BM25.Has("c1") {
		t.Fatal("reloaded index missing c1")
	}

	// Second flush is a no-op (not dirty).
	if err := sn.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndReconcile_rebuildsFromStore(t *testing.T) {
	dir := t.TempDir()
	bm25Path := filepath.Join(dir, "r.bm25")
	hnswPath := filepath.Join(dir, "r.hnsw")

	s := newStore(t)
	a := mkChunk(t, s, "reconcile me into the index")
	b := mkChunk(t, s, "and me too please")

	// Give one chunk a stored embedding to exercise vector reconcile.
	if err := s.SetChunkEmbedding(b.ID, ingest.Float32sToBytes([]float32{1, 2, 3, 4})); err != nil {
		t.Fatal(err)
	}

	idx := search.NewHybridIndex(nil)
	if err := LoadAndReconcile(s, idx, bm25Path, hnswPath); err != nil {
		t.Fatal(err)
	}

	if !idx.BM25.Has(a.ID) || !idx.BM25.Has(b.ID) {
		t.Fatal("BM25 not fully reconciled")
	}
	if !idx.Vector.Has(b.ID) {
		t.Fatal("vector index not reconciled from stored embedding")
	}
	if idx.Vector.Has(a.ID) {
		t.Fatal("chunk a has no embedding; should not be in vector index")
	}
}
