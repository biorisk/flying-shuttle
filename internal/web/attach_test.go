package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestAttachEvidence_wholeChunkAndExcerpt(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}

	bullet, _ := svc.AddRoot("the fear")
	full := "PREAMBLE the words I chose TAIL"
	if err := s.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "iv.txt", Content: full, EndOffset: len(full)}); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	// whole chunk (no offsets)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/outline/nodes/"+bullet.ID+"/evidence",
		url.Values{"chunk_id": {"c1"}}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "datastar-patch-signals") {
		t.Fatalf("whole-chunk attach: %d %s", rec.Code, rec.Body.String())
	}

	forest, _ := svc.Tree()
	if len(forest[0].Children) != 1 {
		t.Fatalf("evidence bullet not attached: %+v", forest[0])
	}
	ev := forest[0].Children[0]
	if ev.Node.Type != model.NodeTypeChunkRef || !ev.Node.Locked {
		t.Fatalf("evidence bullet should be a locked chunk_ref: %+v", ev.Node)
	}
	if ev.Node.Body != full {
		t.Fatalf("whole-chunk body wrong: %q", ev.Node.Body)
	}

	// excerpt
	start := len("PREAMBLE ")
	end := start + len("the words I chose")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/app/outline/nodes/"+bullet.ID+"/evidence",
		url.Values{
			"chunk_id":   {"c1"},
			"char_start": {strconv.Itoa(start)},
			"char_end":   {strconv.Itoa(end)},
			"text":       {"the words I chose"},
		}))
	if rec.Code != 200 {
		t.Fatalf("excerpt attach: %d", rec.Code)
	}
	forest, _ = svc.Tree()
	if len(forest[0].Children) != 2 {
		t.Fatalf("second evidence bullet missing")
	}
	evs, _ := s.ListNodeEvidence(forest[0].Children[1].Node.ID)
	if len(evs) != 1 || evs[0].Text != "the words I chose" || evs[0].CharStart != start {
		t.Fatalf("excerpt evidence row wrong: %+v", evs)
	}
}

func req(t *testing.T, method, path string, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}
