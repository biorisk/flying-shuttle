package search

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
)

// --- BM25 tests ---

func TestBM25_empty(t *testing.T) {
	idx := NewBM25Index()
	results := idx.Search("hello", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestBM25_singleDoc(t *testing.T) {
	idx := NewBM25Index()
	idx.Add("c1", "The quick brown fox jumps over the lazy dog")

	results := idx.Search("quick fox", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ChunkID != "c1" {
		t.Fatalf("expected c1, got %s", results[0].ChunkID)
	}
	if results[0].Score <= 0 {
		t.Fatal("expected positive score")
	}
}

func TestBM25_ranking(t *testing.T) {
	idx := NewBM25Index()
	idx.Add("c1", "Python is a programming language")
	idx.Add("c2", "Python python python programming")
	idx.Add("c3", "Cooking recipes for dinner")

	results := idx.Search("python programming", 10)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// c2 has more "python" occurrences, should rank higher.
	if results[0].ChunkID != "c2" {
		t.Fatalf("expected c2 first, got %s", results[0].ChunkID)
	}
	// "cooking" doc shouldn't match.
	for _, r := range results {
		if r.ChunkID == "c3" {
			t.Fatal("c3 should not match python programming query")
		}
	}
}

func TestBM25_limit(t *testing.T) {
	idx := NewBM25Index()
	for i := 0; i < 20; i++ {
		idx.Add(string(rune('a'+i)), "common word repeated")
	}
	results := idx.Search("common", 5)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
}

func TestBM25_noMatch(t *testing.T) {
	idx := NewBM25Index()
	idx.Add("c1", "apples and oranges")

	results := idx.Search("quantum physics", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// --- Vector index tests ---
// VectorIndex now takes pre-computed float32 vectors directly (no embedder).

func TestVector_empty(t *testing.T) {
	idx := NewVectorIndex()
	// Search on empty index returns nil.
	results := idx.Search([]float32{0, 0, 0, 0, 0, 0, 0, 0}, 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestVector_ranking(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 64}
	ctx := context.Background()

	idx := NewVectorIndex()
	// Add identical text as query to guarantee highest similarity.
	queryText := "quantum physics"
	qVec, _ := embedder.Embed(ctx, queryText)
	idx.Add("c1", qVec) // identical vector — should rank first

	otherVec, _ := embedder.Embed(ctx, "completely unrelated text about cooking")
	idx.Add("c2", otherVec)

	// Search by passing the pre-computed query vector directly.
	results := idx.Search(qVec, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Identical embedding should rank first with similarity ~1.0.
	if results[0].ChunkID != "c1" {
		t.Fatalf("expected c1 first (identical embedding), got %s", results[0].ChunkID)
	}
	if results[0].Score < 0.99 {
		t.Fatalf("expected similarity ~1.0, got %f", results[0].Score)
	}
}

func TestVector_limit(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 8}
	ctx := context.Background()
	idx := NewVectorIndex()
	queryVec, _ := embedder.Embed(ctx, "doc")
	for i := 0; i < 20; i++ {
		vec, _ := embedder.Embed(ctx, "doc")
		idx.Add(string(rune('a'+i)), vec)
	}
	results := idx.Search(queryVec, 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

// --- Hybrid index tests ---

func TestHybrid_empty(t *testing.T) {
	h := NewHybridIndex(&ingest.StubEmbedder{Dim: 8})
	results, err := h.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestHybrid_indexChunks(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 16}
	ctx := context.Background()
	h := NewHybridIndex(embedder)

	vec1, _ := embedder.Embed(ctx, "machine learning neural networks")
	vec2, _ := embedder.Embed(ctx, "cooking pasta Italian food")

	chunks := []model.Chunk{
		{ID: "c1", Content: "machine learning neural networks deep learning", EmbeddingVec: ingest.Float32sToBytes(vec1)},
		{ID: "c2", Content: "cooking pasta Italian food recipes", EmbeddingVec: ingest.Float32sToBytes(vec2)},
	}
	h.IndexChunks(chunks)

	results, err := h.Search(ctx, "machine learning", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// ML chunk should rank first via both BM25 (keyword match) and vector.
	if results[0].ChunkID != "c1" {
		t.Fatalf("expected c1 first, got %s", results[0].ChunkID)
	}
}

func TestHybrid_fusionBoostsMultiSignal(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 16}
	ctx := context.Background()
	h := NewHybridIndex(embedder)

	// c1: matches both keyword and semantic for "quantum"
	// c2: matches keyword only (has word "quantum" but different embedding)
	// c3: matches semantic only (similar embedding but no keyword)
	vec1, _ := embedder.Embed(ctx, "quantum physics theory")
	vec2, _ := embedder.Embed(ctx, "unrelated topic completely")
	vec3, _ := embedder.Embed(ctx, "quantum theory research")

	h.IndexChunk(&model.Chunk{ID: "c1", Content: "quantum physics particle theory", EmbeddingVec: ingest.Float32sToBytes(vec1)})
	h.IndexChunk(&model.Chunk{ID: "c2", Content: "quantum keyword appears here", EmbeddingVec: ingest.Float32sToBytes(vec2)})
	h.IndexChunk(&model.Chunk{ID: "c3", Content: "no keyword match at all", EmbeddingVec: ingest.Float32sToBytes(vec3)})

	results, err := h.Search(ctx, "quantum physics", 10)
	if err != nil {
		t.Fatal(err)
	}
	// c1 should rank first because it appears in both BM25 and vector results.
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].ChunkID != "c1" {
		t.Fatalf("expected c1 first (multi-signal boost), got %s", results[0].ChunkID)
	}
}

// --- RRF tests ---

func TestRRF_singleList(t *testing.T) {
	list := []Result{
		{ChunkID: "a", Score: 10},
		{ChunkID: "b", Score: 5},
	}
	fused := reciprocalRankFusionK(60, list)
	if len(fused) != 2 {
		t.Fatalf("expected 2, got %d", len(fused))
	}
	if fused[0].ChunkID != "a" {
		t.Fatalf("expected a first, got %s", fused[0].ChunkID)
	}
}

func TestRRF_twoLists(t *testing.T) {
	l1 := []Result{
		{ChunkID: "a", Score: 10},
		{ChunkID: "b", Score: 5},
	}
	l2 := []Result{
		{ChunkID: "b", Score: 0.9},
		{ChunkID: "c", Score: 0.5},
	}
	fused := reciprocalRankFusionK(60, l1, l2)
	// "b" appears in both lists, so should get highest fused score:
	// b: 1/(60+2) + 1/(60+1) = 1/62 + 1/61
	// a: 1/(60+1) = 1/61
	// c: 1/(60+2) = 1/62
	if fused[0].ChunkID != "b" {
		t.Fatalf("expected b first (appears in both lists), got %s", fused[0].ChunkID)
	}
}

// --- Tokenizer tests ---

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! This is a test-123.")
	expected := []string{"hello", "world", "this", "is", "a", "test", "123"}
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Fatalf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}
