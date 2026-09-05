package doc

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

func TestNodeCRUD(t *testing.T) {
	s := newTestStore(t)

	n := &model.Node{
		ID:     uuid.NewString(),
		Type:   model.NodeTypeOutline,
		Title:  "Chapter 1",
		Body:   "Opening scene",
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

func TestMoveNode_reorderSiblings(t *testing.T) {
	s := newTestStore(t)

	parent := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Parent"}
	a := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "A"}
	b := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "B"}
	c := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "C"}
	s.CreateNode(parent)
	s.CreateNode(a)
	s.CreateNode(b)
	s.CreateNode(c)

	// Parent -> A(0), B(1), C(2)
	s.CreateEdge(&model.Edge{ID: uuid.NewString(), FromNode: parent.ID, ToNode: a.ID, Type: model.EdgeTypeLinear, Weight: 0})
	s.CreateEdge(&model.Edge{ID: uuid.NewString(), FromNode: parent.ID, ToNode: b.ID, Type: model.EdgeTypeLinear, Weight: 1})
	s.CreateEdge(&model.Edge{ID: uuid.NewString(), FromNode: parent.ID, ToNode: c.ID, Type: model.EdgeTypeLinear, Weight: 2})

	// Move C to position 0 (before A).
	if err := s.MoveNode(c.ID, parent.ID, 0); err != nil {
		t.Fatal(err)
	}

	edges, err := s.ListEdgesFrom(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(edges))
	}
	// Order should be C(0), A(1), B(2).
	order := make(map[string]int)
	for _, e := range edges {
		order[e.ToNode] = e.Weight
	}
	if order[c.ID] != 0 {
		t.Fatalf("expected C at 0, got %d", order[c.ID])
	}
	if order[a.ID] < order[c.ID] {
		t.Fatalf("expected A after C")
	}
}

func TestMoveNode_reparent(t *testing.T) {
	s := newTestStore(t)

	p1 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "P1"}
	p2 := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "P2"}
	child := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Child"}
	s.CreateNode(p1)
	s.CreateNode(p2)
	s.CreateNode(child)

	s.CreateEdge(&model.Edge{ID: uuid.NewString(), FromNode: p1.ID, ToNode: child.ID, Type: model.EdgeTypeLinear, Weight: 0})

	// Move child from P1 to P2.
	if err := s.MoveNode(child.ID, p2.ID, 0); err != nil {
		t.Fatal(err)
	}

	// P1 should have no children.
	from1, _ := s.ListEdgesFrom(p1.ID)
	if len(from1) != 0 {
		t.Fatalf("expected 0 edges from P1, got %d", len(from1))
	}

	// P2 should have 1 child.
	from2, _ := s.ListEdgesFrom(p2.ID)
	if len(from2) != 1 || from2[0].ToNode != child.ID {
		t.Fatalf("expected child under P2, got %v", from2)
	}
}

func TestMoveNode_toRoot(t *testing.T) {
	s := newTestStore(t)

	parent := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Parent"}
	child := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Child"}
	s.CreateNode(parent)
	s.CreateNode(child)

	s.CreateEdge(&model.Edge{ID: uuid.NewString(), FromNode: parent.ID, ToNode: child.ID, Type: model.EdgeTypeLinear, Weight: 0})

	// Move child to root (empty parentID).
	if err := s.MoveNode(child.ID, "", 0); err != nil {
		t.Fatal(err)
	}

	// No incoming edges to child.
	to, _ := s.ListEdgesTo(child.ID)
	if len(to) != 0 {
		t.Fatalf("expected 0 incoming edges, got %d", len(to))
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

func TestSnapshotCRUD(t *testing.T) {
	s := newTestStore(t)

	// Create some data to snapshot.
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Snap Node"}
	s.CreateNode(n)

	// Create a snapshot.
	summary, err := s.CreateSnapshot("v1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Label != "v1" {
		t.Fatalf("expected label v1, got %q", summary.Label)
	}

	// List snapshots.
	list, err := s.ListSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != summary.ID {
		t.Fatalf("expected 1 snapshot, got %d", len(list))
	}

	// Get snapshot with data.
	snap, err := s.GetSnapshot(summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Data.Nodes) != 1 || snap.Data.Nodes[0].Title != "Snap Node" {
		t.Fatalf("snapshot data mismatch: %v", snap.Data.Nodes)
	}

	// Delete snapshot.
	if err := s.DeleteSnapshot(summary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSnapshot(summary.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSnapshotRestore(t *testing.T) {
	s := newTestStore(t)

	// Build an outline: parent -> child, with a thread and chunk association.
	parent := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Parent"}
	child := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: "Child"}
	s.CreateNode(parent)
	s.CreateNode(child)

	e := &model.Edge{ID: uuid.NewString(), FromNode: parent.ID, ToNode: child.ID, Type: model.EdgeTypeLinear, Weight: 0}
	s.CreateEdge(e)

	th := &model.Thread{ID: uuid.NewString(), Name: "Main"}
	s.CreateThread(th)
	s.SetThreadNodes(th.ID, []model.ThreadNode{
		{ThreadID: th.ID, NodeID: parent.ID, Position: 0},
	})

	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "a.txt", Content: "chunk content", EndOffset: 13}
	s.CreateEvidence(&model.Evidence{NodeID: parent.ID, ChunkID: c.ID, SourceFile: c.SourceFile, CharEnd: len([]rune(c.Content)), Text: c.Content})

	// Snapshot this state.
	summary, err := s.CreateSnapshot("before-edits")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate: delete everything.
	s.DeleteEdge(e.ID)
	s.DeleteThread(th.ID)
	s.DeleteNode(child.ID)
	s.DeleteNode(parent.ID)

	// Verify empty.
	nodes, _ := s.ListNodes()
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes after delete, got %d", len(nodes))
	}

	// Restore.
	if err := s.RestoreSnapshot(summary.ID); err != nil {
		t.Fatal(err)
	}

	// Verify restored.
	nodes, err = s.ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes after restore, got %d", len(nodes))
	}

	edges, _ := s.ListEdges()
	if len(edges) != 1 || edges[0].ID != e.ID {
		t.Fatalf("expected 1 edge after restore, got %d", len(edges))
	}

	threads, _ := s.ListThreads()
	if len(threads) != 1 || threads[0].ID != th.ID {
		t.Fatalf("expected 1 thread after restore, got %d", len(threads))
	}

	tn, _ := s.GetThreadNodes(th.ID)
	if len(tn) != 1 || tn[0].NodeID != parent.ID {
		t.Fatalf("expected 1 thread node after restore, got %v", tn)
	}

	ev, _ := s.ListNodeEvidence(parent.ID)
	if len(ev) != 1 || ev[0].ChunkID != c.ID {
		t.Fatalf("expected 1 evidence row after restore, got %v", ev)
	}
}
