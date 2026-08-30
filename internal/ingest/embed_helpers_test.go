package ingest

import (
	"context"
	"testing"
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
