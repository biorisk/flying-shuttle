package dag

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/google/uuid"
)

func newTestStore(t *testing.T) store.Store {
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

func makeNode(t *testing.T, s store.Store, title string) *model.Node {
	t.Helper()
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: title}
	if err := s.CreateNode(n); err != nil {
		t.Fatal(err)
	}
	return n
}

func makeEdge(t *testing.T, s store.Store, from, to string) {
	t.Helper()
	e := &model.Edge{ID: uuid.NewString(), FromNode: from, ToNode: to, Type: model.EdgeTypeLinear}
	if err := s.CreateEdge(e); err != nil {
		t.Fatal(err)
	}
}

func TestWouldCreateCycle(t *testing.T) {
	s := newTestStore(t)

	// A → B → C
	a := makeNode(t, s, "A")
	b := makeNode(t, s, "B")
	c := makeNode(t, s, "C")
	makeEdge(t, s, a.ID, b.ID)
	makeEdge(t, s, b.ID, c.ID)

	// Adding C → A would create a cycle.
	cycle, err := WouldCreateCycle(s, c.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !cycle {
		t.Fatal("expected cycle")
	}

	// Adding A → C would not create a cycle (already implied path).
	cycle, err = WouldCreateCycle(s, a.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cycle {
		t.Fatal("did not expect cycle")
	}

	// Adding D → A should not create a cycle.
	d := makeNode(t, s, "D")
	cycle, err = WouldCreateCycle(s, d.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cycle {
		t.Fatal("did not expect cycle for new node")
	}
}

func TestFindRoots(t *testing.T) {
	s := newTestStore(t)

	a := makeNode(t, s, "A")
	b := makeNode(t, s, "B")
	makeNode(t, s, "C") // isolated node, also a root
	makeEdge(t, s, a.ID, b.ID)

	roots, err := FindRoots(s)
	if err != nil {
		t.Fatal(err)
	}
	// A and C are roots (no incoming edges), B is not.
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(roots))
	}
}

func TestTopologicalSort(t *testing.T) {
	s := newTestStore(t)

	a := makeNode(t, s, "A")
	b := makeNode(t, s, "B")
	c := makeNode(t, s, "C")
	makeEdge(t, s, a.ID, b.ID)
	makeEdge(t, s, b.ID, c.ID)

	sorted, err := TopologicalSort(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3, got %d", len(sorted))
	}
	// A must come before B, B before C.
	idx := map[string]int{}
	for i, n := range sorted {
		idx[n.ID] = i
	}
	if idx[a.ID] >= idx[b.ID] || idx[b.ID] >= idx[c.ID] {
		t.Fatalf("wrong order: A@%d B@%d C@%d", idx[a.ID], idx[b.ID], idx[c.ID])
	}
}

func TestLinearize(t *testing.T) {
	s := newTestStore(t)

	a := makeNode(t, s, "A")
	b := makeNode(t, s, "B")
	th := &model.Thread{ID: uuid.NewString(), Name: "main"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	nodes := []model.ThreadNode{
		{ThreadID: th.ID, NodeID: b.ID, Position: 0},
		{ThreadID: th.ID, NodeID: a.ID, Position: 1},
	}
	if err := s.SetThreadNodes(th.ID, nodes); err != nil {
		t.Fatal(err)
	}

	result, err := Linearize(s, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].ID != b.ID || result[1].ID != a.ID {
		t.Fatalf("unexpected linearization: %v", result)
	}
}
