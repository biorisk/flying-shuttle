package search

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
)

func setupTestTranslator() (*QueryTranslator, context.Context) {
	embedder := &ingest.StubEmbedder{Dim: 16}
	ctx := context.Background()
	idx := NewHybridIndex(embedder)

	// Populate index with sample chunks.
	for _, c := range []model.Chunk{
		{ID: "c1", Content: "The hero overcame fear through courage and determination"},
		{ID: "c2", Content: "Character motivation drives the narrative forward"},
		{ID: "c3", Content: "Scientific method requires hypothesis testing"},
		{ID: "c4", Content: "Emotional growth throughout the hero journey"},
		{ID: "c5", Content: "Baking sourdough bread requires patience and starter"},
	} {
		vec, _ := embedder.Embed(ctx, c.Content)
		c.EmbeddingVec = ingest.Float32sToBytes(vec)
		idx.IndexChunk(&c)
	}

	return &QueryTranslator{Index: idx}, ctx
}

func TestTranslate_basic(t *testing.T) {
	qt, ctx := setupTestTranslator()

	results, err := qt.Translate(ctx, "The Hero's Motivation", "", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected suggestions")
	}
	// Should return Suggestion structs with labels.
	for _, s := range results {
		if s.ChunkID == "" {
			t.Fatal("suggestion has empty chunk_id")
		}
		if s.Label == "" {
			t.Fatal("suggestion has empty label")
		}
		if s.Confidence < 0 || s.Confidence > 1 {
			t.Fatalf("confidence out of range: %f", s.Confidence)
		}
	}
	// First result should have confidence 1.0 (highest score).
	if results[0].Confidence < 0.99 {
		t.Fatalf("expected first result confidence ~1.0, got %f", results[0].Confidence)
	}
}

func TestTranslate_emptyTitle(t *testing.T) {
	qt, ctx := setupTestTranslator()

	results, err := qt.Translate(ctx, "", "", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for empty title, got %d", len(results))
	}
}

func TestTranslate_withBody(t *testing.T) {
	qt, ctx := setupTestTranslator()

	// With body context, should still return results.
	results, err := qt.Translate(ctx, "Hero", "overcoming fear and finding courage", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected suggestions with body context")
	}
}

func TestTranslate_withParent(t *testing.T) {
	qt, ctx := setupTestTranslator()

	// Parent context should expand the query.
	results, err := qt.Translate(ctx, "Motivation", "", "Character Development", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected suggestions with parent context")
	}
}

func TestTranslate_limit(t *testing.T) {
	qt, ctx := setupTestTranslator()

	results, err := qt.Translate(ctx, "hero", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 2 {
		t.Fatalf("expected at most 2, got %d", len(results))
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"The Hero's Motivation", "hero motivation"},
		{"a big red dog", "big red dog"},
		{"", ""},
		{"quantum", "quantum"},
	}
	for _, tt := range tests {
		got := extractKeywords(tt.input)
		if got != tt.expected {
			t.Errorf("extractKeywords(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExpandQueries(t *testing.T) {
	qt := &QueryTranslator{}

	// Title only.
	queries := qt.expandQueries("Hero Motivation", "", "")
	if len(queries) < 2 {
		t.Fatalf("expected at least 2 queries (direct + keywords), got %d", len(queries))
	}

	// Title + body.
	queries = qt.expandQueries("Hero", "overcomes fear", "")
	if len(queries) < 3 {
		t.Fatalf("expected at least 3 queries (direct + keywords + body), got %d", len(queries))
	}

	// Title + parent.
	queries = qt.expandQueries("Motivation", "", "Characters")
	hasParent := false
	for _, q := range queries {
		if q == "Characters Motivation" {
			hasParent = true
		}
	}
	if !hasParent {
		t.Fatal("expected parent-expanded query")
	}

	// Empty title.
	queries = qt.expandQueries("", "", "")
	if len(queries) != 0 {
		t.Fatalf("expected 0 queries for empty title, got %d", len(queries))
	}
}
