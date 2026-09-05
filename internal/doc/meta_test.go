package doc

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
)

func TestMetaKV(t *testing.T) {
	s := newTestStore(t)

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
	s := newTestStore(t)

	if dim, err := s.SampleEmbeddingDim(); err != nil || dim != 0 {
		t.Fatalf("no embeddings: dim=%d err=%v", dim, err)
	}

	if err := s.CreateChunks([]model.Chunk{
		{ID: "a", Content: "x", EmbeddingVec: make([]byte, 768*4)},
		{ID: "b", Content: "y", EmbeddingVec: make([]byte, 768*4)},
		{ID: "c", Content: "z"}, // no vector
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
