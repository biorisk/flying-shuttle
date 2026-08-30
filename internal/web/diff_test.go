package web_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestDiff_renderAndRescue(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}
	a, _ := svc.AddRoot("Kept")
	b, _ := svc.AddChild(a.ID, "Doomed")

	snap, _ := s.CreateSnapshot("baseline")

	// mutate: delete b, add c, rename a
	svc.Delete(b.ID)
	svc.SetTitle(a.ID, "Kept-renamed", 1)
	svc.AddRoot("Fresh")

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/outline?diff="+snap.ID, nil))
	body := rec.Body.String()
	if !strings.Contains(body, "diff-changed") {
		t.Fatalf("renamed bullet should be diff-changed: %s", body)
	}
	if !strings.Contains(body, "diff-added") {
		t.Fatalf("new bullet should be diff-added")
	}
	if !strings.Contains(body, "ghost") || !strings.Contains(body, "Doomed") || !strings.Contains(body, "Rescue") {
		t.Fatalf("deleted bullet should render as a rescuable ghost: %s", body)
	}

	// rescue Doomed (its ghost id == original b.ID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/outline/rescue?diff="+snap.ID+"&node="+b.ID, nil))
	if rec.Code != 200 {
		t.Fatalf("rescue: %d %s", rec.Code, rec.Body.String())
	}
	forest, _ := svc.Tree()
	var found bool
	for _, root := range forest {
		for _, c := range root.Children {
			if c.Node.Title == "Doomed" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("rescued bullet not re-attached under its parent")
	}
}
