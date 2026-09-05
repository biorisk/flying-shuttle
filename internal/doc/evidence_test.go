package doc

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

func TestEvidenceCRUD(t *testing.T) {
	s := newTestStore(t)

	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeChunkRef, Title: "ref"}
	s.CreateNode(n)
	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "a.txt", Content: "the quick brown fox", EndOffset: 19}
	s.CreateChunk(c)

	full := &model.Evidence{
		NodeID: n.ID, ChunkID: c.ID, SourceFile: "a.txt",
		CharStart: 0, CharEnd: 19, Text: "the quick brown fox", Position: 0,
	}
	if err := s.CreateEvidence(full); err != nil {
		t.Fatal(err)
	}
	if full.ID == "" {
		t.Fatal("CreateEvidence did not assign an ID")
	}

	sub := &model.Evidence{
		NodeID: n.ID, ChunkID: c.ID, SourceFile: "a.txt",
		CharStart: 4, CharEnd: 9, Text: "quick", Position: 1,
	}
	if err := s.CreateEvidence(sub); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListNodeEvidence(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "the quick brown fox" || got[1].Text != "quick" {
		t.Fatalf("unexpected evidence: %+v", got)
	}
	if got[1].CharStart != 4 || got[1].CharEnd != 9 {
		t.Fatalf("sub-span offsets wrong: %+v", got[1])
	}

	// GetNodeChunks collapses to the distinct source chunk.
	chunks, _ := s.GetNodeChunks(n.ID)
	if len(chunks) != 1 || chunks[0].ID != c.ID {
		t.Fatalf("expected 1 distinct chunk, got %v", chunks)
	}

	// ListUsedChunkIDs sees the chunk.
	used, _ := s.ListUsedChunkIDs()
	if len(used) != 1 || used[0] != c.ID {
		t.Fatalf("expected c used, got %v", used)
	}

	if err := s.DeleteEvidence(sub.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListNodeEvidence(n.ID)
	if len(got) != 1 {
		t.Fatalf("expected 1 evidence after delete, got %d", len(got))
	}

	if err := s.DeleteNodeEvidence(n.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListNodeEvidence(n.ID)
	if len(got) != 0 {
		t.Fatalf("expected 0 evidence after DeleteNodeEvidence, got %d", len(got))
	}
}

func TestEvidenceSnapshotRoundTrip(t *testing.T) {
	s := newTestStore(t)

	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeChunkRef, Title: "ref"}
	s.CreateNode(n)
	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "a.txt", Content: "hello there world", EndOffset: 17}
	s.CreateChunk(c)
	s.CreateEvidence(&model.Evidence{
		NodeID: n.ID, ChunkID: c.ID, SourceFile: "a.txt",
		CharStart: 6, CharEnd: 11, Text: "there", Position: 0,
	})

	snap, err := s.CreateSnapshot("with-evidence")
	if err != nil {
		t.Fatal(err)
	}
	s.DeleteNodeEvidence(n.ID)
	if err := s.RestoreSnapshot(snap.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := s.ListNodeEvidence(n.ID)
	if len(got) != 1 || got[0].Text != "there" || got[0].CharStart != 6 {
		t.Fatalf("evidence not restored correctly: %+v", got)
	}
}
