package store

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrate(t *testing.T) {
	_ = newTestStore(t) // succeeds without error
}

func TestChunkCRUD(t *testing.T) {
	s := newTestStore(t)

	c := &model.Chunk{
		ID:         uuid.NewString(),
		SourceFile: "test.txt",
		Content:    "hello world",
		EndOffset:  11,
	}
	if err := s.CreateChunk(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChunk(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello world" {
		t.Fatalf("got content %q", got.Content)
	}

	all, err := s.ListChunks()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1, got %d", len(all))
	}
}

func TestNodeCRUD(t *testing.T) {
	s := newTestStore(t)

	n := &model.Node{
		ID:    uuid.NewString(),
		Type:  model.NodeTypeOutline,
		Title: "Chapter 1",
		Body:  "Opening scene",
		Labels: map[string]string{"act": "1"},
	}
	if err := s.CreateNode(n); err != nil {
		t.Fatal(err)
	}
	if n.Version != 1 {
		t.Fatalf("expected version 1, got %d", n.Version)
	}

	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Chapter 1" {
		t.Fatalf("got title %q", got.Title)
	}
	if got.Labels["act"] != "1" {
		t.Fatalf("labels mismatch: %v", got.Labels)
	}

	// Update with correct version
	got.Title = "Chapter 1 (revised)"
	if err := s.UpdateNode(got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}

	// Optimistic concurrency: stale version should fail
	stale := &model.Node{ID: n.ID, Type: model.NodeTypeOutline, Title: "bad", Version: 1}
	if err := s.UpdateNode(stale); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// Delete
	if err := s.DeleteNode(n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(n.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEdgeCRUD(t *testing.T) {
	s := newTestStore(t)

	n1 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "A"}
	n2 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "B"}
	s.CreateNode(n1)
	s.CreateNode(n2)

	e := &model.Edge{
		ID:       uuid.NewString(),
		FromNode: n1.ID,
		ToNode:   n2.ID,
		Type:     model.EdgeTypeLinear,
		Weight:   0,
	}
	if err := s.CreateEdge(e); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetEdge(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FromNode != n1.ID || got.ToNode != n2.ID {
		t.Fatalf("edge mismatch")
	}

	from, err := s.ListEdgesFrom(n1.ID)
	if err != nil || len(from) != 1 {
		t.Fatalf("ListEdgesFrom: err=%v len=%d", err, len(from))
	}

	to, err := s.ListEdgesTo(n2.ID)
	if err != nil || len(to) != 1 {
		t.Fatalf("ListEdgesTo: err=%v len=%d", err, len(to))
	}

	if err := s.DeleteEdge(e.ID); err != nil {
		t.Fatal(err)
	}
}

func TestThreadCRUD(t *testing.T) {
	s := newTestStore(t)

	th := &model.Thread{ID: uuid.NewString(), Name: "Main narrative"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Main narrative" {
		t.Fatalf("got name %q", got.Name)
	}

	got.Name = "Revised narrative"
	if err := s.UpdateThread(got); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNodeChunks(t *testing.T) {
	s := newTestStore(t)

	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeChunkRef, Title: "ref"}
	s.CreateNode(n)

	c1 := &model.Chunk{ID: uuid.NewString(), SourceFile: "a.txt", Content: "aaa", EndOffset: 3}
	c2 := &model.Chunk{ID: uuid.NewString(), SourceFile: "b.txt", Content: "bbb", EndOffset: 3}
	s.CreateChunk(c1)
	s.CreateChunk(c2)

	if err := s.SetNodeChunks(n.ID, []string{c2.ID, c1.ID}); err != nil {
		t.Fatal(err)
	}

	chunks, err := s.GetNodeChunks(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].ID != c2.ID || chunks[1].ID != c1.ID {
		t.Fatalf("unexpected chunks order: %v", chunks)
	}
}

func TestListUsedChunkIDs(t *testing.T) {
	s := newTestStore(t)

	// No used chunks initially.
	used, err := s.ListUsedChunkIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(used) != 0 {
		t.Fatalf("expected 0 used chunks, got %d", len(used))
	}

	// Create nodes and chunks, then associate.
	n1 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeChunkRef, Title: "ref1"}
	n2 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeChunkRef, Title: "ref2"}
	s.CreateNode(n1)
	s.CreateNode(n2)

	c1 := &model.Chunk{ID: uuid.NewString(), SourceFile: "a.txt", Content: "aaa", EndOffset: 3}
	c2 := &model.Chunk{ID: uuid.NewString(), SourceFile: "b.txt", Content: "bbb", EndOffset: 3}
	c3 := &model.Chunk{ID: uuid.NewString(), SourceFile: "c.txt", Content: "ccc", EndOffset: 3}
	s.CreateChunk(c1)
	s.CreateChunk(c2)
	s.CreateChunk(c3)

	// Associate c1 and c2 with n1, c2 with n2 (c2 used by both, c3 unused).
	s.SetNodeChunks(n1.ID, []string{c1.ID, c2.ID})
	s.SetNodeChunks(n2.ID, []string{c2.ID})

	used, err = s.ListUsedChunkIDs()
	if err != nil {
		t.Fatal(err)
	}
	usedSet := make(map[string]bool)
	for _, id := range used {
		usedSet[id] = true
	}

	if !usedSet[c1.ID] {
		t.Fatal("expected c1 to be used")
	}
	if !usedSet[c2.ID] {
		t.Fatal("expected c2 to be used")
	}
	if usedSet[c3.ID] {
		t.Fatal("expected c3 to NOT be used")
	}
	// DISTINCT — c2 should appear only once.
	if len(used) != 2 {
		t.Fatalf("expected 2 distinct used chunks, got %d", len(used))
	}
}

func TestThreadNodes(t *testing.T) {
	s := newTestStore(t)

	th := &model.Thread{ID: uuid.NewString(), Name: "thread"}
	s.CreateThread(th)

	n1 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "A"}
	n2 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "B"}
	s.CreateNode(n1)
	s.CreateNode(n2)

	nodes := []model.ThreadNode{
		{ThreadID: th.ID, NodeID: n1.ID, Position: 0},
		{ThreadID: th.ID, NodeID: n2.ID, Position: 1},
	}
	if err := s.SetThreadNodes(th.ID, nodes); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetThreadNodes(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].NodeID != n1.ID || got[1].NodeID != n2.ID {
		t.Fatalf("unexpected thread nodes: %v", got)
	}
}
