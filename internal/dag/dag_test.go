package dag

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/google/uuid"
)

func newTestStore(t *testing.T) doc.Store {
	t.Helper()
	s, err := doc.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func makeNode(t *testing.T, s doc.Store, title string) *model.Node {
	t.Helper()
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: title}
	if err := s.CreateNode(n); err != nil {
		t.Fatal(err)
	}
	return n
}

func makeEdge(t *testing.T, s doc.Store, from, to string) {
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

func TestValidateGraph_Clean(t *testing.T) {
	s := newTestStore(t)

	a := makeNode(t, s, "A")
	b := makeNode(t, s, "B")
	makeEdge(t, s, a.ID, b.ID)

	report, err := ValidateGraph(s)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("expected valid graph, got issues: %v", report.Issues)
	}
}

func TestValidateGraph_SelfLink(t *testing.T) {
	s := newTestStore(t)

	a := makeNode(t, s, "A")
	// Insert self-link directly via store (bypassing API validation).
	e := &model.Edge{ID: uuid.NewString(), FromNode: a.ID, ToNode: a.ID, Type: model.EdgeTypeLinear}
	if err := s.CreateEdge(e); err != nil {
		t.Fatal(err)
	}

	report, err := ValidateGraph(s)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatal("expected invalid graph")
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Type == "self_link" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected self_link issue, got: %v", report.Issues)
	}
}

func TestValidateGraph_DanglingThreadRef(t *testing.T) {
	s := newTestStore(t)

	a := makeNode(t, s, "A")
	th := &model.Thread{ID: uuid.NewString(), Name: "thread"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	// Point thread at a valid node and a bogus node ID.
	nodes := []model.ThreadNode{
		{ThreadID: th.ID, NodeID: a.ID, Position: 0},
		{ThreadID: th.ID, NodeID: "nonexistent-node-id", Position: 1},
	}
	if err := s.SetThreadNodes(th.ID, nodes); err != nil {
		// FK constraint may block this in SQLite — if so, skip the test.
		t.Skipf("FK constraint prevented dangling ref insertion: %v", err)
	}

	report, err := ValidateGraph(s)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatal("expected invalid graph")
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Type == "orphan_thread_ref" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected orphan_thread_ref issue, got: %v", report.Issues)
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
