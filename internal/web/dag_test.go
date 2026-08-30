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

func dagRouter(t *testing.T) (chi.Router, *outline.Service) {
	t.Helper()
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})
	return r, svc
}

func TestSnapshots_saveRestoreDelete(t *testing.T) {
	r, svc := dagRouter(t)
	a, _ := svc.AddRoot("original")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/snapshots", url.Values{"label": {"v1"}}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="snapshot-bar"`) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	// mutate, then restore
	svc.SetTitle(a.ID, "changed", 1)
	snaps, _ := svc.Store.ListSnapshots()
	if len(snaps) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(snaps))
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/snapshots/"+snaps[0].ID+"/restore", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="outline"`) {
		t.Fatalf("restore didn't repatch outline: %d", rec.Code)
	}
	n, _ := svc.Store.GetNode(a.ID)
	if n.Title != "original" {
		t.Fatalf("restore failed: title=%q", n.Title)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "DELETE", "/app/snapshots/"+snaps[0].ID, nil))
	if rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	snaps, _ = svc.Store.ListSnapshots()
	if len(snaps) != 0 {
		t.Fatalf("snapshot not deleted")
	}
}

func TestBranches_createSwitch(t *testing.T) {
	r, svc := dagRouter(t)
	svc.AddRoot("root")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/branches", url.Values{"name": {"alt"}}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="branch-bar"`) {
		t.Fatalf("create branch: %d %s", rec.Code, rec.Body.String())
	}
	branches, _ := svc.Store.ListBranches()
	if len(branches) == 0 {
		t.Fatalf("no branches created")
	}
	var altID string
	for _, b := range branches {
		if b.Name == "alt" {
			altID = b.ID
		}
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/branches/"+altID+"/switch", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="outline"`) {
		t.Fatalf("switch branch: %d", rec.Code)
	}
}
