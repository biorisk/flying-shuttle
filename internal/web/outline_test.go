package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func outlineRouter(t *testing.T) (chi.Router, *outline.Service) {
	t.Helper()
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})
	return r, svc
}

func TestOutlineFragment_emptyAndPopulated(t *testing.T) {
	r, svc := outlineRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/outline", nil))
	if !strings.Contains(rec.Body.String(), "No outline yet") {
		t.Fatalf("empty outline missing placeholder: %s", rec.Body.String())
	}

	a, _ := svc.AddRoot("Chapter one")
	b, _ := svc.AddChild(a.ID, "a supporting point")
	_ = b

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/outline", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"datastar-patch-elements", `id="outline"`,
		"bullet-" + a.ID, "bullet-" + b.ID,
		"Chapter one", "a supporting point",
		`data-prev-id="` + a.ID + `"`, // b's prev is a
		`data-next-id="` + b.ID + `"`, // a's next is b
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("outline fragment missing %q\n%s", want, body)
		}
	}
}

func TestOutlineFragment_collapseMarkup(t *testing.T) {
	r, svc := outlineRouter(t)
	a, _ := svc.AddRoot("parent")
	svc.AddChild(a.ID, "kid")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/outline", nil))
	// templ HTML-escapes attribute values ("'" -> "&#39;"); assert on the
	// escaped forms the browser will decode.
	body := rec.Body.String()
	esc := strings.NewReplacer("'", "&#39;")
	for _, want := range []string{
		"bullet-toggle",
		esc.Replace("$collapsed['" + a.ID + "'] = !$collapsed['" + a.ID + "']"),
		esc.Replace(`data-show="!$collapsed['` + a.ID + `']"`),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("collapse markup missing %q\n%s", want, body)
		}
	}
}

func TestOutlineFragment_evidenceBinding(t *testing.T) {
	r, svc := outlineRouter(t)
	a, _ := svc.AddRoot("x")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/outline", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "data-on:input__debounce.300ms") {
		t.Fatalf("no debounced input binding:\n%s", body)
	}
	if !strings.Contains(body, "/evidence?node="+a.ID) {
		t.Fatalf("evidence url missing node id:\n%s", body)
	}
}
