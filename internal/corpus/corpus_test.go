package corpus

import (
	"path/filepath"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

func newCorpus(t *testing.T) Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "corpus.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMetaKV(t *testing.T) {
	s := newCorpus(t)
	if v, err := s.GetMeta("embed_model"); err != nil || v != "" {
		t.Fatalf("unset meta: %q %v", v, err)
	}
	if err := s.SetMeta("embed_model", "gemma-768"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("embed_model", "gemma-768-v2"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetMeta("embed_model"); v != "gemma-768-v2" {
		t.Fatalf("upsert: %q", v)
	}
}

func TestClearAllEmbeddingsAndSampleDim(t *testing.T) {
	s := newCorpus(t)
	if dim, err := s.SampleEmbeddingDim(); err != nil || dim != 0 {
		t.Fatalf("no embeddings: dim=%d err=%v", dim, err)
	}
	if err := s.CreateChunks([]model.Chunk{
		{ID: "a", Content: "x", EmbeddingVec: make([]byte, 768*4)},
		{ID: "b", Content: "y", EmbeddingVec: make([]byte, 768*4)},
		{ID: "c", Content: "z"},
	}); err != nil {
		t.Fatal(err)
	}
	if dim, _ := s.SampleEmbeddingDim(); dim != 768 {
		t.Fatalf("sample dim = %d", dim)
	}
	n, err := s.ClearAllEmbeddings()
	if err != nil || n != 2 {
		t.Fatalf("cleared %d (err %v)", n, err)
	}
	if dim, _ := s.SampleEmbeddingDim(); dim != 0 {
		t.Fatalf("dim after clear = %d", dim)
	}
}

func TestChunkCRUD(t *testing.T) {
	s := newCorpus(t)
	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "test.txt", Content: "hello world", EndOffset: 11}
	if err := s.CreateChunk(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChunk(c.ID)
	if err != nil || got.Content != "hello world" {
		t.Fatalf("GetChunk: %+v err=%v", got, err)
	}
	all, err := s.ListChunks()
	if err != nil || len(all) != 1 {
		t.Fatalf("ListChunks: %d err=%v", len(all), err)
	}
}

func TestOpenMigrateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")

	s, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "iv.txt", Content: "hello", EndOffset: 5}
	if err := s.CreateChunk(c); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("embed_model", "test-768"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen read-only: migrations skipped, data still readable.
	ro, err := Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	got, err := ro.GetChunk(c.ID)
	if err != nil || got.Content != "hello" {
		t.Fatalf("GetChunk after reopen: %+v err=%v", got, err)
	}
	if v, _ := ro.GetMeta("embed_model"); v != "test-768" {
		t.Fatalf("meta lost across reopen: %q", v)
	}
}
