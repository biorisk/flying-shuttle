package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestStitchPreview_manuscript(t *testing.T) {
	s, _ := doc.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}

	b1, _ := svc.AddRoot("point one")
	s.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "iv.txt", Content: "the first thing said", EndOffset: 20})
	s.CreateChunk(&model.Chunk{ID: "c2", SourceFile: "iv.txt", Content: "the second thing said", EndOffset: 21})
	svc.AttachEvidence(b1.ID, "c1", 0, 0, "")
	svc.AttachEvidence(b1.ID, "c2", 0, 0, "")

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stitch?glue=50", nil))
	body := rec.Body.String()
	for _, w := range []string{
		"datastar-patch-elements", `id="stitch"`,
		"the first thing said", "the second thing said",
		"span-chunk", "span-glue", // stub stitcher inserts " ... " glue
		"% glue", "chars",
	} {
		if !strings.Contains(body, w) {
			t.Fatalf("stitch preview missing %q\n%s", w, body)
		}
	}
}

func TestStitchPreview_exportLink(t *testing.T) {
	s, _ := doc.NewSQLiteStore(":memory:")
	s.Migrate()
	t.Cleanup(func() { s.Close() })
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stitch", nil))
	body := rec.Body.String()
	for _, w := range []string{"/export.md", "/export.html", "/export.pdf", "data-attr:href"} {
		if !strings.Contains(body, w) {
			t.Fatalf("export link %q missing:\n%s", w, body)
		}
	}
}
