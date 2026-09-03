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
	r.ServeHTTP(rec, req(t, "POST", "/outline/nodes/"+bullet.ID+"/evidence",
		url.Values{"chunk_id": {"c1"}}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "datastar-patch-signals") {
		t.Fatalf("whole-chunk attach: %d %s", rec.Code, rec.Body.String())
	}

	forest, _ := svc.Tree()
	if len(forest[0].Children) != 1 {
		t.Fatalf("evidence bullet not attached: %+v", forest[0])
	}
	ev := forest[0].Children[0]
	if ev.Node.Type != model.NodeTypeChunkRef {
		t.Fatalf("evidence bullet should be a chunk_ref: %+v", ev.Node)
	}
	if ev.Node.Body != full {
		t.Fatalf("whole-chunk body wrong: %q", ev.Node.Body)
	}

	// excerpt
	start := len("PREAMBLE ")
	end := start + len("the words I chose")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/outline/nodes/"+bullet.ID+"/evidence",
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

func TestAttachEvidence_fromCandidateSelection(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}
	bullet, _ := svc.AddRoot("claim")
	full := "Opening remarks. The witness confirmed the payment was authorized. Closing."
	if err := s.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "iv.txt", Content: full, EndOffset: len(full)}); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	// The candidate-card path posts only chunk_id + text (no offsets).
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/outline/nodes/"+bullet.ID+"/evidence", url.Values{
		"chunk_id": {"c1"},
		"text":     {"the payment was authorized"},
	}))
	if rec.Code != 200 {
		t.Fatalf("attach from text: %d %s", rec.Code, rec.Body.String())
	}
	forest, _ := svc.Tree()
	evs, _ := s.ListNodeEvidence(forest[0].Children[0].Node.ID)
	if len(evs) != 1 || evs[0].Text != "the payment was authorized" {
		t.Fatalf("evidence not attached from text: %+v", evs)
	}
	if string([]rune(full)[evs[0].CharStart:evs[0].CharEnd]) != evs[0].Text {
		t.Fatalf("offsets do not match text: %+v", evs[0])
	}
}

func TestQuoteEditAndDelete(t *testing.T) {
	s, _ := store.NewSQLiteStore(":memory:")
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	svc := &outline.Service{Store: s}

	bullet, _ := svc.AddRoot("claim")
	full := "LEAD the decisive testimony came late TAIL"
	if err := s.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "iv.txt", Content: full, EndOffset: len(full)}); err != nil {
		t.Fatal(err)
	}
	quote := "the decisive testimony came late"
	ev, err := svc.AttachEvidence(bullet.ID, "c1", len("LEAD "), len("LEAD ")+len(quote), quote)
	if err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Outline: svc})

	// trim "the " off the front and " came late" off the back -> "decisive testimony"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/outline/quote-edit", url.Values{
		"node_id": {ev.ID}, "op": {"trim"},
		"start": {"4"}, "end": {strconv.Itoa(len("the decisive testimony"))},
	}))
	if rec.Code != 200 {
		t.Fatalf("quote-edit trim: %d %s", rec.Code, rec.Body.String())
	}
	evs, _ := s.ListNodeEvidence(ev.ID)
	if len(evs) != 1 || evs[0].Text != "decisive testimony" {
		t.Fatalf("after trim: %+v", evs)
	}

	// splice "decisive " out -> "testimony"
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/outline/quote-edit", url.Values{
		"node_id": {ev.ID}, "op": {"splice"}, "start": {"0"}, "end": {"9"},
	}))
	if rec.Code != 200 {
		t.Fatalf("quote-edit splice: %d", rec.Code)
	}
	if n, _ := s.GetNode(ev.ID); n.Body != "testimony" {
		t.Fatalf("after splice, node body = %q", n.Body)
	}

	// delete the quote via the X button's endpoint
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "DELETE", "/outline/nodes/"+ev.ID, nil))
	if rec.Code != 200 {
		t.Fatalf("delete quote: %d", rec.Code)
	}
	forest, _ := svc.Tree()
	if len(forest[0].Children) != 0 {
		t.Fatalf("quote not deleted: %+v", forest[0].Children)
	}
	if evs, _ := s.ListNodeEvidence(ev.ID); len(evs) != 0 {
		t.Fatalf("evidence row not cascaded: %+v", evs)
	}
}

func req(t *testing.T, method, path string, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}
