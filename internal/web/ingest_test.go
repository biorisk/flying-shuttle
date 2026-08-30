package web_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/pipeline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func ingestRouter(t *testing.T) (chi.Router, store.Store) {
	t.Helper()
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dir := t.TempDir()
	r := chi.NewRouter()
	web.Mount(r, web.Deps{
		Store:    s,
		Ingester: &pipeline.Ingester{Store: s, UploadDir: dir},
	})
	return r, s
}

func TestIngestGet_empty(t *testing.T) {
	r, _ := ingestRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ingest", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "No transcripts loaded yet") {
		t.Fatalf("unexpected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIngestUpload_txt(t *testing.T) {
	r, s := ingestRouter(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "interview.txt")
	fw.Write([]byte("First paragraph about fear.\n\nSecond paragraph about resolve and courage."))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/ingest", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "interview.txt") {
		t.Fatalf("drawer patch missing filename: %s", rec.Body.String())
	}

	// Processing is async; wait for it to land chunks.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cs, _ := s.ListChunks()
		if len(cs) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no chunks produced from upload")
}

func TestIngestUpload_rejectsNonText(t *testing.T) {
	r, s := ingestRouter(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "audio.mp3")
	fw.Write([]byte("not really audio"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/ingest", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	ups, _, _ := s.ListUploadsPage(10, 0)
	if len(ups) != 0 {
		t.Fatalf("mp3 should have been rejected, got %d uploads", len(ups))
	}
}

func TestIngestUpload_batchSummaryAndPoll(t *testing.T) {
	r, s := ingestRouter(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	const n = 12
	for i := 0; i < n; i++ {
		fw, _ := mw.CreateFormFile("files", "t"+string(rune('a'+i))+".txt")
		fw.Write([]byte("Para one about fear.\n\nPara two about resolve."))
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/ingest", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "12 file(s)") {
		t.Fatalf("summary should show the batch total:\n%s", rec.Body.String())
	}

	// Poll GET /ingest until all done; the poll attribute must be present
	// while anything is still processing and gone once finished.
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		g := httptest.NewRecorder()
		r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/ingest", nil))
		last = g.Body.String()
		hasPoll := strings.Contains(last, "data-on-interval__duration.2s")
		if strings.Contains(last, "12 done") {
			if hasPoll {
				t.Fatalf("poll attribute should be gone once all done:\n%s", last)
			}
			ups, total, _ := s.ListUploadsPage(0, 0)
			if total != n || len(ups) != n {
				t.Fatalf("want %d uploads, got total=%d rows=%d", n, total, len(ups))
			}
			return
		}
		if !hasPoll {
			t.Fatalf("poll attribute missing while still processing:\n%s", last)
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("batch never finished:\n%s", last)
}
