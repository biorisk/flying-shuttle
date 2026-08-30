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
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
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

func TestEvidenceRoute_blankQuery(t *testing.T) {
	r := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/evidence", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	// SSE patch carrying the idle placeholder fragment.
	body := rec.Body.String()
	if !strings.Contains(body, "datastar-patch-elements") || !strings.Contains(body, `id="evidence"`) {
		t.Fatalf("not an evidence patch: %s", body)
	}
	if !strings.Contains(body, "Start typing a bullet") {
		t.Fatalf("blank query should render idle prompt: %s", body)
	}
}

func TestEvidenceRoute_withQueryNoIndex(t *testing.T) {
	r := testRouter(t) // Deps.Index is nil
	req := httptest.NewRequest(http.MethodGet, "/evidence?q=fear&node=n1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	// nil index -> no candidates -> "no passages found" branch
	if !strings.Contains(rec.Body.String(), "No passages found") {
		t.Fatalf("expected empty-results branch: %s", rec.Body.String())
	}
}
