package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func editRouter(t *testing.T) (chi.Router, *outline.Service) {
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

func do(t *testing.T, r chi.Router, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestOutlineEdit_addRootThenSiblingAndChild(t *testing.T) {
	r, svc := editRouter(t)

	rec := do(t, r, "POST", "/outline/roots", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "datastar-patch-signals") {
		t.Fatalf("add root: %d %s", rec.Code, rec.Body.String())
	}
	forest, _ := svc.Tree()
	if len(forest) != 1 {
		t.Fatalf("want 1 root, got %d", len(forest))
	}
	rootID := forest[0].Node.ID

	// sibling, carrying a title for the anchor
	rec = do(t, r, "POST", "/outline/nodes/"+rootID+"/sibling",
		url.Values{"title": {"Chapter one"}, "version": {"1"}})
	if rec.Code != 200 {
		t.Fatalf("sibling: %d", rec.Code)
	}
	forest, _ = svc.Tree()
	if len(forest) != 2 {
		t.Fatalf("want 2 roots, got %d", len(forest))
	}
	if forest[0].Node.Title != "Chapter one" {
		t.Fatalf("anchor title not persisted: %q", forest[0].Node.Title)
	}

	// child of the first root
	rec = do(t, r, "POST", "/outline/nodes/"+rootID+"/child", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("child: %d", rec.Code)
	}
	forest, _ = svc.Tree()
	if len(forest[0].Children) != 1 {
		t.Fatalf("want 1 child, got %d", len(forest[0].Children))
	}
}

func TestOutlineEdit_indentUnindentDelete(t *testing.T) {
	r, svc := editRouter(t)
	a, _ := svc.AddRoot("a")
	b, _ := svc.AddSibling(a.ID, "b")

	rec := do(t, r, "POST", "/outline/nodes/"+b.ID+"/indent", url.Values{"title": {"b"}, "version": {"1"}})
	if rec.Code != 200 {
		t.Fatalf("indent: %d", rec.Code)
	}
	forest, _ := svc.Tree()
	if len(forest) != 1 || len(forest[0].Children) != 1 {
		t.Fatalf("indent failed: %+v", forest)
	}

	rec = do(t, r, "POST", "/outline/nodes/"+b.ID+"/unindent", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("unindent: %d", rec.Code)
	}
	forest, _ = svc.Tree()
	if len(forest) != 2 {
		t.Fatalf("unindent failed: %+v", forest)
	}

	rec = do(t, r, "DELETE", "/outline/nodes/"+b.ID, nil)
	if rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	forest, _ = svc.Tree()
	if len(forest) != 1 || forest[0].Node.ID != a.ID {
		t.Fatalf("delete failed: %+v", forest)
	}
}

func TestOutlineEdit_setTitle(t *testing.T) {
	r, svc := editRouter(t)
	a, _ := svc.AddRoot("orig")

	rec := do(t, r, "PATCH", "/outline/nodes/"+a.ID,
		url.Values{"title": {"renamed"}, "version": {"1"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("patch title: %d %s", rec.Code, rec.Body.String())
	}
	n, _ := svc.Store.GetNode(a.ID)
	if n.Title != "renamed" {
		t.Fatalf("title not saved: %q", n.Title)
	}

	// stale version -> full resync fragment
	rec = do(t, r, "PATCH", "/outline/nodes/"+a.ID,
		url.Values{"title": {"again"}, "version": {"1"}})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="outline"`) {
		t.Fatalf("stale patch should resync: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOutlineEdit_indentFirstSiblingIsNoop(t *testing.T) {
	r, svc := editRouter(t)
	a, _ := svc.AddRoot("a")
	svc.AddSibling(a.ID, "b")

	rec := do(t, r, "POST", "/outline/nodes/"+a.ID+"/indent", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("noop indent should 200, got %d", rec.Code)
	}
	forest, _ := svc.Tree()
	if len(forest) != 2 {
		t.Fatalf("noop indent changed structure: %+v", forest)
	}
}
