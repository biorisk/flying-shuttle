package web_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func projectRouter(t *testing.T) (chi.Router, string, *[]string) {
	t.Helper()
	s, _ := doc.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "default"), 0o755)

	switched := &[]string{}
	r := chi.NewRouter()
	web.Mount(r, web.Deps{
		Store:         s,
		ProjectName:   "default",
		ProjectHome:   home,
		SwitchProject: func(name string) { *switched = append(*switched, name) },
	})
	return r, home, switched
}

func TestProjectSwitch(t *testing.T) {
	r, _, switched := projectRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/project/switch?name=other", nil))
	if rec.Code != 200 {
		t.Fatalf("switch: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Switching to other") || !strings.Contains(body, "<script") || !strings.Contains(body, "location.reload") {
		t.Fatalf("switch fragment wrong:\n%s", body)
	}
	// give the goroutine a beat
	deadlineWait(t, func() bool { return len(*switched) == 1 && (*switched)[0] == "other" })

	// switching to the current project is a no-op
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/project/switch?name=default", nil))
	if rec.Code != 204 {
		t.Fatalf("no-op switch should be 204, got %d", rec.Code)
	}

	// invalid names rejected
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/project/switch?name=../evil", nil))
	if rec.Code != 400 {
		t.Fatalf("path traversal should 400, got %d", rec.Code)
	}
}

func TestProjectNew_createsDir(t *testing.T) {
	r, home, switched := projectRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/new", strings.NewReader("name=fresh-book"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("new: %d %s", rec.Code, rec.Body.String())
	}
	if fi, err := os.Stat(filepath.Join(home, "fresh-book")); err != nil || !fi.IsDir() {
		t.Fatalf("project dir not created")
	}
	deadlineWait(t, func() bool { return len(*switched) == 1 && (*switched)[0] == "fresh-book" })
}

func TestProjectBar_rendersInShell(t *testing.T) {
	r, _, _ := projectRouter(t)
	os.MkdirAll(filepath.Dir(filepath.Join("x")), 0o755) // noop
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="project-bar"`) || !strings.Contains(body, "project-select") {
		t.Fatalf("shell missing project picker:\n%s", body[:min(2000, len(body))])
	}
}

func deadlineWait(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met")
}
