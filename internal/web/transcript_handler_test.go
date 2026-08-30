package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
)

func TestTranscriptReader_windowAndScrub(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	pos := 0
	var ids []string
	for i := 0; i < 8; i++ {
		txt := "seg" + string(rune('A'+i))
		c := &model.Chunk{ID: txt, SourceFile: "iv.txt", Content: txt, StartOffset: pos, EndOffset: pos + len(txt)}
		if err := s.CreateChunk(c); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
		pos += len(txt) + 1
	}

	r := chi.NewRouter()
	web.Mount(r, web.Deps{Store: s})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/evidence/transcript?chunk=segE&node=n1", nil))
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
