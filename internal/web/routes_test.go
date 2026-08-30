package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func testRouter(t *testing.T) chi.Router {
	t.Helper()
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s})
	return r
}

func TestShellRoute(t *testing.T) {
	r := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /app/ = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="shell"`, `id="outline"`, `id="evidence"`, `id="ingest-drawer"`,
		"data-signals=", web.DatastarScriptPath,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell HTML missing %q", want)
		}
	}
}

func TestStaticRoute(t *testing.T) {
	r := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/static/vendor/datastar-v1.0.3.js", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET datastar runtime = %d", rec.Code)
	}
	b, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(b), "Datastar v1.0.3") {
		t.Fatalf("unexpected runtime body: %.60s", b)
	}
}
