package search

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
)

func TestBM25_AddIdempotent(t *testing.T) {
	idx := NewBM25Index()
	idx.Add("a", "the quick brown fox")
	idx.Add("a", "the quick brown fox") // re-add same id
	idx.Add("b", "lazy dog sleeps")

	if idx.Len() != 2 {
		t.Fatalf("Len = %d, want 2", idx.Len())
	}
	if !idx.Has("a") || !idx.Has("b") {
		t.Fatal("Has missing ids")
	}
	// "fox" should resolve to exactly one doc, not a phantom duplicate.
	res := idx.Search("fox", 10)
	if len(res) != 1 || res[0].ChunkID != "a" {
		t.Fatalf("Search(fox) = %+v", res)
	}
}

func TestBM25_AddReplacesContent(t *testing.T) {
	idx := NewBM25Index()
	idx.Add("a", "alpha beta")
	idx.Add("a", "gamma delta") // replace

	if got := idx.Search("alpha", 10); len(got) != 0 {
		t.Fatalf("stale term still indexed: %+v", got)
	}
	if got := idx.Search("gamma", 10); len(got) != 1 {
		t.Fatalf("new term not indexed: %+v", got)
	}
}

func TestBM25_SaveLoadRoundTrip(t *testing.T) {
	idx := NewBM25Index()
	for _, d := range []struct{ id, text string }{
		{"c1", "machine learning models"},
		{"c2", "distributed systems and consensus"},
		{"c3", "learning to rank search results"},
	} {
		idx.Add(d.id, d.text)
	}
	want := idx.Search("learning", 10)

	var buf bytes.Buffer
	if err := idx.Save(&buf); err != nil {
		t.Fatal(err)
	}

	restored := NewBM25Index()
	if err := restored.Load(&buf); err != nil {
		t.Fatal(err)
	}
	if restored.Len() != idx.Len() {
		t.Fatalf("Len %d != %d", restored.Len(), idx.Len())
	}
	got := restored.Search("learning", 10)
	if len(got) != len(want) {
		t.Fatalf("result count %d != %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ChunkID != want[i].ChunkID || got[i].Score != want[i].Score {
			t.Fatalf("result %d: %+v != %+v", i, got[i], want[i])
		}
	}
}

func TestHybridIndex_SnapshotAndReload(t *testing.T) {
	dir := t.TempDir()
	bm25Path := filepath.Join(dir, "shuttle.bm25")
	hnswPath := filepath.Join(dir, "shuttle.hnsw")

	idx := NewHybridIndex(nil)
	idx.IndexChunk(&model.Chunk{ID: "c1", Content: "quantum entanglement experiments"})
	vec := make([]float32, 8)
	vec[0] = 1
	idx.IndexChunk(&model.Chunk{ID: "c2", Content: "with a vector", EmbeddingVec: ingest.Float32sToBytes(vec)})

	if !idx.Dirty() {
		t.Fatal("index should be dirty after IndexChunk")
	}
	if err := idx.SnapshotBM25(bm25Path); err != nil {
		t.Fatal(err)
	}
	if err := idx.SnapshotVector(hnswPath); err != nil {
		t.Fatal(err)
	}

	// Files exist and are non-empty.
	for _, p := range []string{bm25Path, hnswPath} {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() == 0 {
			t.Fatalf("snapshot %s missing or empty (%v)", p, err)
		}
	}

	reloaded := NewHybridIndex(nil)
	if ok, err := reloaded.LoadBM25(bm25Path); err != nil || !ok {
		t.Fatalf("LoadBM25: ok=%v err=%v", ok, err)
	}
	if ok, err := reloaded.LoadVector(hnswPath); err != nil || !ok {
		t.Fatalf("LoadVector: ok=%v err=%v", ok, err)
	}
	if !reloaded.BM25.Has("c1") {
		t.Error("reloaded BM25 missing c1")
	}
	if !reloaded.Vector.Has("c2") {
		t.Error("reloaded vector index missing c2")
	}
}

func TestHybridIndex_ConcurrentIndexSearchSnapshot(t *testing.T) {
	idx := NewHybridIndex(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "c.bm25")

	done := make(chan struct{})
	go func() { // writer
		for i := 0; i < 500; i++ {
			idx.IndexChunk(&model.Chunk{ID: string(rune('a'+i%26)) + "-" + itoa(i), Content: "term content here"})
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
			idx.BM25.Search("content", 5)
			_ = idx.SnapshotBM25(path)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestHybridIndex_LoadMissingFileIsNotError(t *testing.T) {
	idx := NewHybridIndex(nil)
	ok, err := idx.LoadBM25(filepath.Join(t.TempDir(), "nope.bm25"))
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want false,nil", ok, err)
	}
}
