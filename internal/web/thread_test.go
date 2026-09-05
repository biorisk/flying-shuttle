package web_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/storetest"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestThreads_createToggleAppend(t *testing.T) {
	sp := storetest.New(t)
	s := sp.Doc
	svc := &outline.Service{Store: s, Corpus: sp.Corpus}
	a, _ := svc.AddRoot("a")
	b, _ := svc.AddSibling(a.ID, "b")

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Corpus: sp.Corpus, Outline: svc})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/threads", url.Values{"name": {"Ch1"}}))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "datastar-patch-signals") {
		t.Fatalf("create thread: %d %s", rec.Code, rec.Body.String())
	}
	threads, _ := s.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("want 1 thread")
	}
	tid := threads[0].ID

	// toggle a in
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/threads/"+tid+"/nodes/"+a.ID+"/toggle", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="outline"`) {
		t.Fatalf("toggle: %d", rec.Code)
	}
	tns, _ := s.GetThreadNodes(tid)
	if len(tns) != 1 || tns[0].NodeID != a.ID {
		t.Fatalf("toggle-in failed: %+v", tns)
	}

	// append b (brush)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/threads/"+tid+"/nodes/"+b.ID+"/append", nil))
	if rec.Code != 200 {
		t.Fatalf("append: %d", rec.Code)
	}
	tns, _ = s.GetThreadNodes(tid)
	if len(tns) != 2 || tns[1].NodeID != b.ID || tns[1].Position != 1 {
		t.Fatalf("append failed: %+v", tns)
	}

	// toggle a out
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req(t, "POST", "/threads/"+tid+"/nodes/"+a.ID+"/toggle", nil))
	tns, _ = s.GetThreadNodes(tid)
	if len(tns) != 1 || tns[0].NodeID != b.ID {
		t.Fatalf("toggle-out failed: %+v", tns)
	}

	// outline scoped to thread marks membership
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/outline?thread="+tid, nil))
	body := rec.Body.String()
	if !strings.Contains(body, `data-thread="`+tid+`"`) {
		t.Fatalf("outline not thread-scoped: %s", body)
	}
	if !strings.Contains(body, "in-thread") {
		t.Fatalf("no in-thread bullet class: %s", body)
	}
}
