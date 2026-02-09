package ingest

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
)

func TestCosineSimilarity_identical(t *testing.T) {
	a := []float32{1, 0, 0}
	sim := CosineSimilarity(a, a)
	if sim < 0.999 {
		t.Fatalf("expected ~1, got %f", sim)
	}
}

func TestCosineSimilarity_orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := CosineSimilarity(a, b)
	if sim > 0.001 || sim < -0.001 {
		t.Fatalf("expected ~0, got %f", sim)
	}
}

func TestCosineSimilarity_opposite(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{-1, 0, 0}
	sim := CosineSimilarity(a, b)
	if sim > -0.999 {
		t.Fatalf("expected ~-1, got %f", sim)
	}
}

func TestFloat32sRoundTrip(t *testing.T) {
	orig := []float32{1.5, -2.3, 0, 42.0}
	b := Float32sToBytes(orig)
	got := BytesToFloat32s(b)
	if len(got) != len(orig) {
		t.Fatalf("length mismatch: %d vs %d", len(got), len(orig))
	}
	for i := range orig {
		if got[i] != orig[i] {
			t.Fatalf("index %d: got %f, want %f", i, got[i], orig[i])
		}
	}
}

func TestStubEmbedder_deterministic(t *testing.T) {
	e := &StubEmbedder{Dim: 8}
	ctx := context.Background()
	v1, _ := e.Embed(ctx, "hello world")
	v2, _ := e.Embed(ctx, "hello world")
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("non-deterministic at index %d: %f vs %f", i, v1[i], v2[i])
		}
	}
}

func TestStubEmbedder_different_inputs(t *testing.T) {
	e := &StubEmbedder{Dim: 8}
	ctx := context.Background()
	v1, _ := e.Embed(ctx, "hello world")
	v2, _ := e.Embed(ctx, "completely different text about science")
	// They should differ.
	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different inputs produced identical embeddings")
	}
}

func TestChunker_singleSegment(t *testing.T) {
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 16},
		Config:   ChunkerConfig{Threshold: 0.5, MinSentences: 1},
	}
	segs := []model.TranscriptSegment{
		{ID: "s1", UploadID: "u1", Speaker: "Alice", Text: "Hello world", StartMs: 0, EndMs: 1000},
	}

	chunks, err := c.ChunkSegments(context.Background(), "test.wav", segs)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "Hello world" {
		t.Fatalf("unexpected content: %s", chunks[0].Content)
	}
	if chunks[0].Speaker == nil || *chunks[0].Speaker != "Alice" {
		t.Fatal("expected speaker Alice")
	}
	if chunks[0].StartOffset != 0 || chunks[0].EndOffset != 1000 {
		t.Fatalf("unexpected offsets: %d-%d", chunks[0].StartOffset, chunks[0].EndOffset)
	}
}

func TestChunker_emptySegments(t *testing.T) {
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 16},
		Config:   ChunkerConfig{},
	}
	chunks, err := c.ChunkSegments(context.Background(), "test.wav", nil)
	if err != nil {
		t.Fatal(err)
	}
	if chunks != nil {
		t.Fatalf("expected nil, got %d chunks", len(chunks))
	}
}

func TestChunker_multipleSimilarSegments(t *testing.T) {
	// Segments with very similar text should stay in one chunk when threshold is high.
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 64},
		Config:   ChunkerConfig{Threshold: 1.5, MinSentences: 1}, // very high threshold — no splits
	}
	segs := []model.TranscriptSegment{
		{ID: "s1", Speaker: "Bob", Text: "The cat sat on the mat", StartMs: 0, EndMs: 1000},
		{ID: "s2", Speaker: "Bob", Text: "The cat lay on the mat", StartMs: 1000, EndMs: 2000},
		{ID: "s3", Speaker: "Bob", Text: "The cat slept on the mat", StartMs: 2000, EndMs: 3000},
	}

	chunks, err := c.ChunkSegments(context.Background(), "cats.wav", segs)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk with high threshold, got %d", len(chunks))
	}
	if chunks[0].StartOffset != 0 || chunks[0].EndOffset != 3000 {
		t.Fatalf("unexpected offsets: %d-%d", chunks[0].StartOffset, chunks[0].EndOffset)
	}
}

func TestChunker_lowThresholdSplits(t *testing.T) {
	// With threshold 0 every pair is a boundary (distance is always > 0 for different texts).
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 64},
		Config:   ChunkerConfig{Threshold: 0.0001, MinSentences: 1},
	}
	segs := []model.TranscriptSegment{
		{ID: "s1", Speaker: "A", Text: "Quantum physics is fascinating", StartMs: 0, EndMs: 1000},
		{ID: "s2", Speaker: "B", Text: "I love cooking pasta dishes", StartMs: 1000, EndMs: 2000},
		{ID: "s3", Speaker: "A", Text: "The weather is nice today", StartMs: 2000, EndMs: 3000},
	}

	chunks, err := c.ChunkSegments(context.Background(), "mixed.wav", segs)
	if err != nil {
		t.Fatal(err)
	}
	// With near-zero threshold each distinct text gets its own chunk.
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks with low threshold, got %d", len(chunks))
	}
}

func TestChunker_minSentencesRespected(t *testing.T) {
	// Even with threshold 0, minSentences=3 should keep everything in one chunk
	// when there are only 3 segments.
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 64},
		Config:   ChunkerConfig{Threshold: 0.0001, MinSentences: 3},
	}
	segs := []model.TranscriptSegment{
		{ID: "s1", Speaker: "A", Text: "Topic one about science", StartMs: 0, EndMs: 1000},
		{ID: "s2", Speaker: "B", Text: "Topic two about cooking", StartMs: 1000, EndMs: 2000},
		{ID: "s3", Speaker: "A", Text: "Topic three about weather", StartMs: 2000, EndMs: 3000},
	}

	chunks, err := c.ChunkSegments(context.Background(), "test.wav", segs)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (minSentences=3), got %d", len(chunks))
	}
}

func TestChunker_embeddingVecPopulated(t *testing.T) {
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 16},
		Config:   ChunkerConfig{Threshold: 1.5, MinSentences: 1},
	}
	segs := []model.TranscriptSegment{
		{ID: "s1", Speaker: "A", Text: "Hello there", StartMs: 0, EndMs: 500},
	}

	chunks, err := c.ChunkSegments(context.Background(), "t.wav", segs)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks[0].EmbeddingVec) == 0 {
		t.Fatal("expected embedding_vec to be populated")
	}
	vec := BytesToFloat32s(chunks[0].EmbeddingVec)
	if len(vec) != 16 {
		t.Fatalf("expected 16-dim embedding, got %d", len(vec))
	}
}

func TestChunker_chunkIDsUnique(t *testing.T) {
	c := &Chunker{
		Embedder: &StubEmbedder{Dim: 16},
		Config:   ChunkerConfig{Threshold: 0.0001, MinSentences: 1},
	}
	segs := []model.TranscriptSegment{
		{ID: "s1", Text: "Alpha topic", StartMs: 0, EndMs: 500},
		{ID: "s2", Text: "Beta completely different topic", StartMs: 500, EndMs: 1000},
		{ID: "s3", Text: "Gamma yet another topic", StartMs: 1000, EndMs: 1500},
	}

	chunks, err := c.ChunkSegments(context.Background(), "t.wav", segs)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool)
	for _, ch := range chunks {
		if ch.ID == "" {
			t.Fatal("chunk has empty ID")
		}
		if ids[ch.ID] {
			t.Fatalf("duplicate chunk ID: %s", ch.ID)
		}
		ids[ch.ID] = true
	}
}
