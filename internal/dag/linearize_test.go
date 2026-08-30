package dag

import (
	"context"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
)

func setupLinearizeStore(t *testing.T) store.Store {
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

func TestLinearizeAndStitch_threadMode(t *testing.T) {
	s := setupLinearizeStore(t)

	// Create two nodes with body text.
	n1 := &model.Node{ID: "n1", Type: "outline", Title: "Intro", Body: "Once upon a time"}
	n2 := &model.Node{ID: "n2", Type: "outline", Title: "Middle", Body: "The hero journeyed"}
	if err := s.CreateNode(n1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNode(n2); err != nil {
		t.Fatal(err)
	}

	// Create a thread with both nodes.
	th := &model.Thread{ID: "t1", Name: "Main"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadNodes("t1", []model.ThreadNode{
		{ThreadID: "t1", NodeID: "n1", Position: 0},
		{ThreadID: "t1", NodeID: "n2", Position: 1},
	}); err != nil {
		t.Fatal(err)
	}

	stitcher := &stitch.StubStitcher{}
	result, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode:     ModeThread,
		ThreadID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(result.Nodes))
	}
	if result.Nodes[0].ID != "n1" {
		t.Fatalf("expected first node n1, got %s", result.Nodes[0].ID)
	}
	if result.Stitch == nil {
		t.Fatal("stitch result is nil")
	}
	if result.Stitch.Stats.TotalChars == 0 {
		t.Fatal("expected non-zero total chars")
	}
}

func TestLinearizeAndStitch_manuscriptMode(t *testing.T) {
	s := setupLinearizeStore(t)

	// Create nodes with edges defining order.
	n1 := &model.Node{ID: "n1", Type: "outline", Title: "Chapter 1", Body: "Beginning"}
	n2 := &model.Node{ID: "n2", Type: "outline", Title: "Chapter 2", Body: "Middle"}
	n3 := &model.Node{ID: "n3", Type: "outline", Title: "Chapter 3", Body: "End"}
	for _, n := range []*model.Node{n1, n2, n3} {
		if err := s.CreateNode(n); err != nil {
			t.Fatal(err)
		}
	}
	// n1 → n2 → n3
	if err := s.CreateEdge(&model.Edge{ID: "e1", FromNode: "n1", ToNode: "n2", Type: "linear", Weight: 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEdge(&model.Edge{ID: "e2", FromNode: "n2", ToNode: "n3", Type: "linear", Weight: 0}); err != nil {
		t.Fatal(err)
	}

	stitcher := &stitch.StubStitcher{}
	result, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode: ModeManuscript,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(result.Nodes))
	}
	// Topological order: n1 before n2 before n3.
	if result.Nodes[0].ID != "n1" || result.Nodes[1].ID != "n2" || result.Nodes[2].ID != "n3" {
		t.Fatalf("unexpected order: %s, %s, %s", result.Nodes[0].ID, result.Nodes[1].ID, result.Nodes[2].ID)
	}
}

func TestLinearizeAndStitch_withChunks(t *testing.T) {
	s := setupLinearizeStore(t)

	// Create a node and associate chunks.
	n1 := &model.Node{ID: "n1", Type: "chunk_ref", Title: "Evidence"}
	if err := s.CreateNode(n1); err != nil {
		t.Fatal(err)
	}

	c1 := &model.Chunk{ID: "c1", SourceFile: "interview.txt", Content: "I felt afraid"}
	c2 := &model.Chunk{ID: "c2", SourceFile: "interview.txt", Content: "But I pushed through"}
	if err := s.CreateChunk(c1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateChunk(c2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeChunks("n1", []string{"c1", "c2"}); err != nil {
		t.Fatal(err)
	}

	th := &model.Thread{ID: "t1", Name: "Test"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadNodes("t1", []model.ThreadNode{
		{ThreadID: "t1", NodeID: "n1", Position: 0},
	}); err != nil {
		t.Fatal(err)
	}

	stitcher := &stitch.StubStitcher{}
	result, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode:     ModeThread,
		ThreadID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should use chunks, not node body.
	if result.Stitch.Stats.ChunkChars == 0 {
		t.Fatal("expected chunk chars from associated chunks")
	}
	// Text should contain both chunks.
	text := result.Stitch.Text
	if len(text) == 0 {
		t.Fatal("expected non-empty stitched text")
	}
}

func TestLinearizeAndStitch_sharedChunkDedup(t *testing.T) {
	s := setupLinearizeStore(t)

	// Two nodes share the same chunk.
	n1 := &model.Node{ID: "n1", Type: "chunk_ref", Title: "Node A"}
	n2 := &model.Node{ID: "n2", Type: "chunk_ref", Title: "Node B"}
	if err := s.CreateNode(n1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNode(n2); err != nil {
		t.Fatal(err)
	}

	c1 := &model.Chunk{ID: "c1", SourceFile: "f.txt", Content: "shared content"}
	if err := s.CreateChunk(c1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeChunks("n1", []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeChunks("n2", []string{"c1"}); err != nil {
		t.Fatal(err)
	}

	th := &model.Thread{ID: "t1", Name: "Test"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadNodes("t1", []model.ThreadNode{
		{ThreadID: "t1", NodeID: "n1", Position: 0},
		{ThreadID: "t1", NodeID: "n2", Position: 1},
	}); err != nil {
		t.Fatal(err)
	}

	stitcher := &stitch.StubStitcher{}
	result, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode:     ModeThread,
		ThreadID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Shared chunk c1 should appear only once in stitched output.
	chunkCount := 0
	for _, span := range result.Stitch.Spans {
		if span.Type == stitch.SpanChunk {
			chunkCount++
		}
	}
	if chunkCount != 1 {
		t.Fatalf("expected 1 chunk span (deduped), got %d", chunkCount)
	}
}

func TestLinearizeAndStitch_emptyThread(t *testing.T) {
	s := setupLinearizeStore(t)

	th := &model.Thread{ID: "t1", Name: "Empty"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}

	stitcher := &stitch.StubStitcher{}
	result, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode:     ModeThread,
		ThreadID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Nodes) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(result.Nodes))
	}
	if result.Stitch.Stats.TotalChars != 0 {
		t.Fatalf("expected 0 total chars, got %d", result.Stitch.Stats.TotalChars)
	}
}

func TestLinearizeAndStitch_missingThreadID(t *testing.T) {
	s := setupLinearizeStore(t)
	stitcher := &stitch.StubStitcher{}

	_, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode: ModeThread,
	})
	if err == nil {
		t.Fatal("expected error for missing thread_id")
	}
}

func TestLinearizeAndStitch_glueLevel(t *testing.T) {
	s := setupLinearizeStore(t)

	n1 := &model.Node{ID: "n1", Type: "outline", Title: "A", Body: "Alpha"}
	n2 := &model.Node{ID: "n2", Type: "outline", Title: "B", Body: "Beta"}
	if err := s.CreateNode(n1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNode(n2); err != nil {
		t.Fatal(err)
	}

	th := &model.Thread{ID: "t1", Name: "Test"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadNodes("t1", []model.ThreadNode{
		{ThreadID: "t1", NodeID: "n1", Position: 0},
		{ThreadID: "t1", NodeID: "n2", Position: 1},
	}); err != nil {
		t.Fatal(err)
	}

	// StubStitcher always adds " ... " glue between chunks.
	stitcher := &stitch.StubStitcher{}
	result, err := LinearizeAndStitch(context.Background(), s, stitcher, LinearizeRequest{
		Mode:      ModeThread,
		ThreadID:  "t1",
		GlueLevel: 75,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify glue is present.
	if result.Stitch.Stats.GlueChars == 0 {
		t.Fatal("expected glue chars from stub stitcher")
	}
}

// TestLinearizeAndStitch_usesExcerptNotWholeChunk is the fidelity regression
// guard for the evidence-as-text-span change: when a node cites only part of a
// chunk, the stitched manuscript must contain that part and NOT the rest.
func TestLinearizeAndStitch_usesExcerptNotWholeChunk(t *testing.T) {
	s := setupLinearizeStore(t)

	n1 := &model.Node{ID: "n1", Type: "chunk_ref", Title: "Evidence"}
	if err := s.CreateNode(n1); err != nil {
		t.Fatal(err)
	}
	full := "SECRET_PREAMBLE the part the writer actually chose SECRET_TAIL"
	c := &model.Chunk{ID: "c1", SourceFile: "interview.txt", Content: full}
	if err := s.CreateChunk(c); err != nil {
		t.Fatal(err)
	}

	// Attach only the middle span as evidence.
	const want = "the part the writer actually chose"
	start := len([]rune("SECRET_PREAMBLE "))
	if err := s.CreateEvidence(&model.Evidence{
		NodeID: "n1", ChunkID: "c1", SourceFile: "interview.txt",
		CharStart: start, CharEnd: start + len([]rune(want)), Text: want, Position: 0,
	}); err != nil {
		t.Fatal(err)
	}

	th := &model.Thread{ID: "t1", Name: "T"}
	s.CreateThread(th)
	s.SetThreadNodes("t1", []model.ThreadNode{{ThreadID: "t1", NodeID: "n1", Position: 0}})

	result, err := LinearizeAndStitch(context.Background(), s, &stitch.StubStitcher{}, LinearizeRequest{
		Mode: ModeThread, ThreadID: "t1", GlueLevel: 50,
	})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Stitch.Text
	if !strings.Contains(text, want) {
		t.Fatalf("stitched text missing the chosen excerpt: %q", text)
	}
	if strings.Contains(text, "SECRET_PREAMBLE") || strings.Contains(text, "SECRET_TAIL") {
		t.Fatalf("stitched text leaked un-chosen chunk text: %q", text)
	}
}
