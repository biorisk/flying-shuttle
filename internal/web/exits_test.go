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

func TestExits_addListDelete(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}
	a, _ := svc.AddRoot("Scene A")
	b, _ := svc.AddSibling(a.ID, "Scene B")

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	// list exits (empty) — B is an option
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/app/outline/nodes/"+a.ID+"/exits", nil))
	if !strings.Contains(rec.Body.String(), "Scene B") || !strings.Contains(rec.Body.String(), `id="exits-`+a.ID+`"`) {
		t.Fatalf("exits fragment wrong: %s", rec.Body.String())
	}

	// add a branch exit A -> B if brave
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/outline/nodes/"+a.ID+"/exits", url.Values{
		"to_node": {b.ID}, "type": {"branch"}, "condition": {"brave"},
	}))
	if rec.Code != 200 {
		t.Fatalf("add exit: %d %s", rec.Code, rec.Body.String())
	}
	edges, _ := s.ListEdgesFrom(a.ID)
	var edgeID string
	for _, e := range edges {
		if string(e.Type) == "branch" && e.ToNode == b.ID {
			edgeID = e.ID
		}
	}
	if edgeID == "" {
		t.Fatalf("branch edge not created: %+v", edges)
	}
	if !strings.Contains(rec.Body.String(), "if brave") {
		t.Fatalf("condition not rendered: %s", rec.Body.String())
	}

	// delete it
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "DELETE", "/app/outline/edges/"+edgeID+"?node="+a.ID, nil))
	if rec.Code != 200 {
		t.Fatalf("delete exit: %d", rec.Code)
	}
	edges, _ = s.ListEdgesFrom(a.ID)
	for _, e := range edges {
		if e.ID == edgeID {
			t.Fatalf("edge not deleted")
		}
	}
}
