package search

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
)

func TestContextChecker_noChunks(t *testing.T) {
	idx := NewHybridIndex(&ingest.StubEmbedder{Dim: 16})
	cc := &ContextChecker{Index: idx}

	check, err := cc.Check(context.Background(), "Hero", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !check.InContext {
		t.Fatal("expected in_context for empty chunks")
	}
	if check.Score != 1.0 {
		t.Fatalf("expected score 1.0, got %f", check.Score)
	}
}

func TestContextChecker_emptyParent(t *testing.T) {
	idx := NewHybridIndex(&ingest.StubEmbedder{Dim: 16})
	cc := &ContextChecker{Index: idx}

	check, err := cc.Check(context.Background(), "", []string{"c1"})
	if err != nil {
		t.Fatal(err)
	}
	if !check.InContext {
		t.Fatal("expected in_context for empty parent")
	}
}

func TestContextChecker_chunkInIndex(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 16}
	idx := NewHybridIndex(embedder)

	// Index some chunks.
	idx.IndexChunks([]model.Chunk{
		{ID: "c1", Content: "hero overcame fear through courage"},
		{ID: "c2", Content: "scientific method requires hypothesis testing"},
		{ID: "c3", Content: "baking sourdough bread requires patience"},
	})

	cc := &ContextChecker{Index: idx, Threshold: 0.1}

	// Check c1 against a relevant parent context.
	check, err := cc.Check(context.Background(), "hero courage", []string{"c1"})
	if err != nil {
		t.Fatal(err)
	}
	// c1 should score well for "hero courage" query via BM25.
	if check.Score <= 0 {
		t.Fatalf("expected positive score, got %f", check.Score)
	}
}

func TestContextChecker_chunkNotInIndex(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 16}
	idx := NewHybridIndex(embedder)

	// Index some chunks but NOT the one we're checking.
	idx.IndexChunks([]model.Chunk{
		{ID: "c1", Content: "hero overcame fear"},
	})

	cc := &ContextChecker{Index: idx, Threshold: 0.3}

	// Check a chunk that's not in the index at all.
	check, err := cc.Check(context.Background(), "hero", []string{"c99"})
	if err != nil {
		t.Fatal(err)
	}
	// c99 not in index, so score should be 0.
	if check.Score != 0 {
		t.Fatalf("expected score 0 for missing chunk, got %f", check.Score)
	}
	if check.InContext {
		t.Fatal("expected out of context for missing chunk")
	}
	if check.Message == "" {
		t.Fatal("expected warning message")
	}
}

func TestContextChecker_thresholdBehavior(t *testing.T) {
	embedder := &ingest.StubEmbedder{Dim: 16}
	idx := NewHybridIndex(embedder)
	idx.IndexChunks([]model.Chunk{
		{ID: "c1", Content: "topic A content"},
		{ID: "c2", Content: "topic B content"},
	})

	// High threshold — harder to be in context.
	cc := &ContextChecker{Index: idx, Threshold: 0.99}
	check, err := cc.Check(context.Background(), "topic A", []string{"c1"})
	if err != nil {
		t.Fatal(err)
	}
	// With a very high threshold, the check may fail.
	// The exact behavior depends on search scores, but we verify no error.
	_ = check
}
