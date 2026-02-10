package search

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

func TestEmbeddingClusterer_empty(t *testing.T) {
	ec := &EmbeddingClusterer{Embedder: &ingest.StubEmbedder{Dim: 16}}
	clusters, err := ec.Cluster(context.Background(), "Hero", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestEmbeddingClusterer_single(t *testing.T) {
	ec := &EmbeddingClusterer{Embedder: &ingest.StubEmbedder{Dim: 16}}
	clusters, err := ec.Cluster(context.Background(), "Hero", []ChunkWithContent{
		{ID: "c1", Content: "The hero overcame fear", Score: 0.9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	if clusters[0].ChunkCount != 1 {
		t.Fatalf("expected 1 chunk, got %d", clusters[0].ChunkCount)
	}
	if clusters[0].ChunkIDs[0] != "c1" {
		t.Fatalf("expected c1, got %s", clusters[0].ChunkIDs[0])
	}
}

func TestEmbeddingClusterer_multiple(t *testing.T) {
	ec := &EmbeddingClusterer{
		Embedder:    &ingest.StubEmbedder{Dim: 16},
		MaxClusters: 3,
	}
	chunks := []ChunkWithContent{
		{ID: "c1", Content: "The hero overcame fear through courage", Score: 0.95},
		{ID: "c2", Content: "Character motivation drives the narrative", Score: 0.85},
		{ID: "c3", Content: "Scientific method requires hypothesis testing", Score: 0.7},
		{ID: "c4", Content: "Emotional growth throughout the journey", Score: 0.8},
		{ID: "c5", Content: "Baking sourdough bread requires patience", Score: 0.5},
	}
	clusters, err := ec.Cluster(context.Background(), "Hero", chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) == 0 {
		t.Fatal("expected at least 1 cluster")
	}
	if len(clusters) > 3 {
		t.Fatalf("expected at most 3 clusters, got %d", len(clusters))
	}

	// All chunks should be assigned to exactly one cluster.
	seen := make(map[string]bool)
	for _, cl := range clusters {
		for _, id := range cl.ChunkIDs {
			if seen[id] {
				t.Fatalf("chunk %s appears in multiple clusters", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 chunks assigned, got %d", len(seen))
	}
}

func TestEmbeddingClusterer_labelsIncludeTitle(t *testing.T) {
	ec := &EmbeddingClusterer{Embedder: &ingest.StubEmbedder{Dim: 16}}
	clusters, err := ec.Cluster(context.Background(), "Motivation", []ChunkWithContent{
		{ID: "c1", Content: "Inner drive", Score: 0.9},
		{ID: "c2", Content: "External pressure", Score: 0.8},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cl := range clusters {
		if cl.Label == "" {
			t.Fatal("cluster has empty label")
		}
		// Label should contain the node title.
		if len(cl.Label) < len("Motivation") {
			t.Fatalf("label too short: %q", cl.Label)
		}
	}
}

func TestEmbeddingClusterer_confidence(t *testing.T) {
	ec := &EmbeddingClusterer{Embedder: &ingest.StubEmbedder{Dim: 16}}
	clusters, err := ec.Cluster(context.Background(), "Test", []ChunkWithContent{
		{ID: "c1", Content: "Alpha content", Score: 0.9},
		{ID: "c2", Content: "Beta content", Score: 0.7},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cl := range clusters {
		if cl.Confidence < 0 || cl.Confidence > 1 {
			t.Fatalf("confidence out of range: %f", cl.Confidence)
		}
	}
}

// --- LLM clusterer tests ---

type mockClusterCompleter struct {
	response string
}

func (m *mockClusterCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	return m.response, nil
}

func TestLLMClusterer_parsesResponse(t *testing.T) {
	lc := &LLMClusterer{Complete: &mockClusterCompleter{
		response: `[
			{"label": "Past Trauma", "chunk_indices": [0, 2], "confidence": 0.9},
			{"label": "Need for Approval", "chunk_indices": [1], "confidence": 0.75}
		]`,
	}}
	chunks := []ChunkWithContent{
		{ID: "c1", Content: "childhood fears"},
		{ID: "c2", Content: "seeking validation"},
		{ID: "c3", Content: "painful memories"},
	}
	clusters, err := lc.Cluster(context.Background(), "Hero Backstory", chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	if clusters[0].ChunkCount != 2 {
		t.Fatalf("expected 2 chunks in first cluster, got %d", clusters[0].ChunkCount)
	}
	if clusters[0].ChunkIDs[0] != "c1" || clusters[0].ChunkIDs[1] != "c3" {
		t.Fatalf("wrong chunk IDs: %v", clusters[0].ChunkIDs)
	}
	if clusters[1].ChunkIDs[0] != "c2" {
		t.Fatalf("wrong chunk ID in second cluster: %v", clusters[1].ChunkIDs)
	}
}

func TestLLMClusterer_codeFences(t *testing.T) {
	lc := &LLMClusterer{Complete: &mockClusterCompleter{
		response: "```json\n" + `[{"label": "Theme", "chunk_indices": [0], "confidence": 0.8}]` + "\n```",
	}}
	clusters, err := lc.Cluster(context.Background(), "Test", []ChunkWithContent{
		{ID: "c1", Content: "content"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
}

func TestParseClusters_invalidJSON(t *testing.T) {
	_, err := parseClusters("not json", "Test", nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseClusters_outOfBoundsIndex(t *testing.T) {
	clusters, err := parseClusters(
		`[{"label":"A","chunk_indices":[0,99],"confidence":0.8}]`,
		"Test",
		[]ChunkWithContent{{ID: "c1", Content: "x"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Chunk index 99 is out of bounds, only c1 should be included.
	if clusters[0].ChunkCount != 1 {
		t.Fatalf("expected 1 valid chunk, got %d", clusters[0].ChunkCount)
	}
}
