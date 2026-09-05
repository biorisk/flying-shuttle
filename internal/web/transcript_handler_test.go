package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/storetest"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestTranscriptReader_windowAndScrub(t *testing.T) {
	sp := storetest.New(t)
	s := sp.Doc

	pos := 0
	var ids []string
	for i := 0; i < 8; i++ {
		txt := "seg" + string(rune('A'+i))
		c := &model.Chunk{ID: txt, SourceFile: "iv.txt", Content: txt, StartOffset: pos, EndOffset: pos + len(txt)}
		if err := sp.Corpus.CreateChunk(c); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
		pos += len(txt) + 1
	}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Corpus: sp.Corpus})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/evidence/transcript?chunk=segE&node=n1", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"datastar-patch-elements", `id="transcript-reader"`,
		"datastar-patch-signals", "readerChunk", "segE",
		`data-chunk="segE"`, "reader-seg focus",
		"segD", "segF", // context on either side
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reader fragment missing %q\n%s", want, body)
		}
	}
	// earlier/later scrub buttons target neighbours
	if !strings.Contains(body, "chunk=segB") || !strings.Contains(body, "chunk=segH") {
		t.Fatalf("scrub buttons missing neighbour chunk ids: %s", body)
	}
}

func TestTranscriptReader_highlightsLocatedSpan(t *testing.T) {
	sp := storetest.New(t)
	s := sp.Doc
	sp.Corpus.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "a.txt", Content: "alpha beta gamma delta", StartOffset: 0, EndOffset: 22})
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Corpus: sp.Corpus})

	rec := httptest.NewRecorder()
	// fs/fe select "beta gamma" (runes 6..16).
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/evidence/transcript?chunk=c1&node=n1&fs=6&fe=16", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `<mark id=\"reader-focus\"`) && !strings.Contains(body, `<mark id="reader-focus"`) {
		t.Fatalf("reader missing focus mark:\n%s", body)
	}
	if !strings.Contains(body, "beta gamma") {
		t.Fatalf("focus text not rendered:\n%s", body)
	}

	// No fs/fe → no focus mark.
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/evidence/transcript?chunk=c1&node=n1", nil))
	if strings.Contains(rec2.Body.String(), "reader-focus") {
		t.Fatalf("unexpected focus mark without fs/fe")
	}
}

func TestTranscriptReader_prefillsExcerptFormWithLocatedSpan(t *testing.T) {
	sp := storetest.New(t)
	s := sp.Doc
	sp.Corpus.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "a.txt", Content: "alpha beta gamma delta", StartOffset: 0, EndOffset: 22})
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Corpus: sp.Corpus})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/evidence/transcript?chunk=c1&node=n1&fs=6&fe=16", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`data-prefilled="1"`,
		`name="char_start" value="6"`,
		`name="char_end" value="16"`,
		`name="text" value="beta gamma"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("excerpt prefill missing %q\n%s", want, body)
		}
	}

	// Without fs/fe the hidden offsets stay empty (whole-chunk attach).
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/evidence/transcript?chunk=c1&node=n1", nil))
	if b := rec2.Body.String(); strings.Contains(b, `data-prefilled="1"`) || strings.Contains(b, `name="char_start" value="0"`) {
		t.Fatalf("unexpected prefill without fs/fe:\n%s", b)
	}
}

func TestTranscriptReader_hasExcerptForm(t *testing.T) {
	sp := storetest.New(t)
	s := sp.Doc
	sp.Corpus.CreateChunk(&model.Chunk{ID: "c1", SourceFile: "a.txt", Content: "hello world here", StartOffset: 0, EndOffset: 16})
	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s, Corpus: sp.Corpus})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/evidence/transcript?chunk=c1&node=n9", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`id="excerpt-form"`, `name="chunk_id"`, `name="char_start"`, `name="text"`,
		"/outline/nodes/", "/evidence", `value="c1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("excerpt form missing %q\n%s", want, body)
		}
	}
}
