package dag

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
)

func TestEvalCondition_empty(t *testing.T) {
	if !EvalCondition("", nil) {
		t.Fatal("empty condition should always match")
	}
}

func TestEvalCondition_audienceEqual(t *testing.T) {
	ctx := &EvalContext{Audience: "novice"}
	if !EvalCondition("audience==novice", ctx) {
		t.Fatal("should match audience==novice")
	}
	if EvalCondition("audience==expert", ctx) {
		t.Fatal("should not match audience==expert")
	}
}

func TestEvalCondition_audienceNotEqual(t *testing.T) {
	ctx := &EvalContext{Audience: "novice"}
	if !EvalCondition("audience!=expert", ctx) {
		t.Fatal("should match audience!=expert")
	}
	if EvalCondition("audience!=novice", ctx) {
		t.Fatal("should not match audience!=novice")
	}
}

func TestEvalCondition_hasRead(t *testing.T) {
	ctx := &EvalContext{ReadNodes: map[string]bool{"n1": true}}
	if !EvalCondition("has_read(n1)", ctx) {
		t.Fatal("should match has_read(n1)")
	}
	if EvalCondition("has_read(n2)", ctx) {
		t.Fatal("should not match has_read(n2)")
	}
}

func TestEvalCondition_notHasRead(t *testing.T) {
	ctx := &EvalContext{ReadNodes: map[string]bool{"n1": true}}
	if EvalCondition("!has_read(n1)", ctx) {
		t.Fatal("should not match !has_read(n1)")
	}
	if !EvalCondition("!has_read(n2)", ctx) {
		t.Fatal("should match !has_read(n2)")
	}
}

func TestEvalCondition_label(t *testing.T) {
	ctx := &EvalContext{Labels: map[string]string{"difficulty": "hard"}}
	if !EvalCondition("label.difficulty==hard", ctx) {
		t.Fatal("should match label.difficulty==hard")
	}
	if EvalCondition("label.difficulty==easy", ctx) {
		t.Fatal("should not match label.difficulty==easy")
	}
}

func TestEvalCondition_unknownDefaultsTrue(t *testing.T) {
	if !EvalCondition("some_unknown_thing", nil) {
		t.Fatal("unknown condition should default to true")
	}
}

func TestConditionalWalk_basicPath(t *testing.T) {
	s := setupStore(t)
	createNodes(t, s, "n1", "n2", "n3")
	createEdge(t, s, "e1", "n1", "n2", "linear", nil, 0)
	createEdge(t, s, "e2", "n2", "n3", "linear", nil, 0)

	nodes, err := ConditionalWalk(s, []string{"n1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
}

func TestConditionalWalk_audienceFiltering(t *testing.T) {
	s := setupStore(t)
	createNodes(t, s, "n1", "n2-novice", "n3-expert")
	novice := "audience==novice"
	expert := "audience==expert"
	createEdge(t, s, "e1", "n1", "n2-novice", "branch", &novice, 0)
	createEdge(t, s, "e2", "n1", "n3-expert", "branch", &expert, 1)

	// Walk as novice — should see n1, n2-novice.
	nodes, err := ConditionalWalk(s, []string{"n1"}, &EvalContext{Audience: "novice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes for novice, got %d", len(nodes))
	}
	if nodes[1].ID != "n2-novice" {
		t.Fatalf("expected n2-novice, got %s", nodes[1].ID)
	}

	// Walk as expert — should see n1, n3-expert.
	nodes, err = ConditionalWalk(s, []string{"n1"}, &EvalContext{Audience: "expert"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes for expert, got %d", len(nodes))
	}
	if nodes[1].ID != "n3-expert" {
		t.Fatalf("expected n3-expert, got %s", nodes[1].ID)
	}
}

func TestConditionalWalk_hasReadGating(t *testing.T) {
	s := setupStore(t)
	createNodes(t, s, "n1", "n2", "n3")
	cond := "has_read(n2)"
	createEdge(t, s, "e1", "n1", "n2", "linear", nil, 0)
	createEdge(t, s, "e2", "n1", "n3", "branch", &cond, 1)

	// Walk from n1 — n2 has no condition so gets visited, then n3's condition
	// (has_read(n2)) is evaluated after n2 is visited, so n3 should appear.
	nodes, err := ConditionalWalk(s, []string{"n1"}, &EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
}

// --- helpers ---

func setupStore(t *testing.T) store.Store {
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

func createNodes(t *testing.T, s store.Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := s.CreateNode(&model.Node{ID: id, Type: "outline", Title: id}); err != nil {
			t.Fatal(err)
		}
	}
}

func createEdge(t *testing.T, s store.Store, id, from, to string, typ model.EdgeType, cond *string, weight int) {
	t.Helper()
	if err := s.CreateEdge(&model.Edge{ID: id, FromNode: from, ToNode: to, Type: typ, Condition: cond, Weight: weight}); err != nil {
		t.Fatal(err)
	}
}
