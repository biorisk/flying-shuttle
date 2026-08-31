package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func previewRouter(t *testing.T) (chi.Router, *outline.Service, string) {
	t.Helper()
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dir := t.TempDir()
	omd := filepath.Join(dir, "outline.md")
	os.WriteFile(omd, []byte("# my-book\n\n- Chapter one `[locked]`\n  > a quote — _iv.txt_\n"), 0o644)
	svc := &outline.Service{Store: s}
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc, ProjectName: "my-book", OutlineMDPath: omd})
	return r, svc, omd
}

func TestPreview_outlineHTMLandControls(t *testing.T) {
	r, _, _ := previewRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/outline.html", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("status/ct: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	h := rec.Body.String()
	for _, w := range []string{
		"<h1", "my-book", "<blockquote>",
		`name="doc-width"`, "letter", "landscape", "fit",
		`href="/outline.html"`, `href="/outline.md"`, `href="/outline.pdf"`,
		`class="active"`, "preview.events",
	} {
		if !strings.Contains(h, w) {
			t.Fatalf("outline.html missing %q", w)
		}
	}
}

func TestPreview_outlineRawAndPDF(t *testing.T) {
	r, _, _ := previewRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/outline.md", nil))
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" ||
		!strings.Contains(rec.Body.String(), "# my-book") ||
		strings.Contains(rec.Body.String(), "<h1") {
		t.Fatalf("raw view wrong: %s | %s", rec.Header().Get("Content-Type"), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/outline.pdf", nil))
	if rec.Header().Get("Content-Type") != "application/pdf" || rec.Body.Len() < 500 ||
		rec.Body.String()[:4] != "%PDF" {
		t.Fatalf("pdf wrong: %s %d", rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

func TestPreview_manuscriptHTMLandPDF(t *testing.T) {
	r, svc, _ := previewRouter(t)
	b, _ := svc.AddRoot("A point")
	c := &model.Chunk{ID: "c1", SourceFile: "iv.txt", Content: "the exact words"}
	svc.Store.CreateChunk(c)
	svc.AttachEvidence(b.ID, "c1", 0, 0, "")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/export.html?glue=50", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "the exact words") {
		t.Fatalf("manuscript html: %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/export.md?glue=50&amp;inline=1") {
		t.Fatalf("raw tab should point at inline export.md:\n%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/export.pdf?glue=50", nil))
	if rec.Header().Get("Content-Type") != "application/pdf" || rec.Body.String()[:4] != "%PDF" {
		t.Fatalf("manuscript pdf wrong")
	}

	// /export.md still downloads by default, inline=1 serves text/plain
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/export.md", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export.md should still be a download")
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/export.md?inline=1", nil))
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("export.md?inline=1 should be text/plain")
	}
}

func TestPreviewEvents_reloadOnBroadcast(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	s.Migrate()
	t.Cleanup(func() { s.Close() })
	br := web.NewBroadcaster()
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, PreviewReload: br})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest("GET", srv.URL+"/preview.events", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		total := ""
		for {
			n, err := resp.Body.Read(buf)
			total += string(buf[:n])
			if strings.Contains(total, "event: reload") {
				got <- total
				return
			}
			if err != nil {
				got <- total
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	br.Notify()

	select {
	case s := <-got:
		if !strings.Contains(s, "event: reload") {
			t.Fatalf("no reload event: %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reload event")
	}
}
