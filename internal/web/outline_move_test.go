package web_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestOutlineMove_reparentAndReorder(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}
	a, _ := svc.AddRoot("a")
	b, _ := svc.AddSibling(a.ID, "b")
	c, _ := svc.AddSibling(b.ID, "c")

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	// move c under a at position 0
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/outline/move", url.Values{
		"node_id": {c.ID}, "parent_id": {a.ID}, "position": {"0"},
	}))
	if rec.Code != 200 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	forest, _ := svc.Tree()
	if len(forest) != 2 || len(forest[0].Children) != 1 || forest[0].Children[0].Node.ID != c.ID {
		t.Fatalf("reparent failed: %v", forest)
	}

	// moving a node under its own descendant is rejected (no-op, still 200)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/outline/move", url.Values{
		"node_id": {a.ID}, "parent_id": {c.ID}, "position": {"0"},
	}))
	if rec.Code != 200 {
		t.Fatalf("self-descendant move status: %d", rec.Code)
	}
	forest, _ = svc.Tree()
	if forest[0].Node.ID != a.ID || len(forest[0].Children) != 1 {
		t.Fatalf("self-descendant move should be a no-op: %v", forest)
	}
	_ = b
}

func TestOutlineFragment_hasDragHandleAndMoveForm(t *testing.T) {
	r, svc := outlineRouter(t)
	svc.AddRoot("x")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/outline", nil))
	body := rec.Body.String()
	for _, w := range []string{`id="move-form"`, `class="drag-handle"`, `draggable="true"`} {
		if !strings.Contains(body, w) {
			t.Fatalf("missing %q", w)
		}
	}
}
