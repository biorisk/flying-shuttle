package transcript

import (
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/model"
)

func setup(t *testing.T) (*Service, *doc.SQLiteStore) {
	t.Helper()
	s, err := doc.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Service{Store: s}, s
}

// seedTranscript writes n sequential chunks for one source file, each holding
// "cK" as its text, with contiguous offsets.
func seed(t *testing.T, s *doc.SQLiteStore, file string, n int) []model.Chunk {
	t.Helper()
	var chunks []model.Chunk
	pos := 0
	for i := 0; i < n; i++ {
		txt := "chunk" + string(rune('A'+i))
		c := model.Chunk{
			ID: file + "-" + txt, SourceFile: file, Content: txt,
			StartOffset: pos, EndOffset: pos + len(txt),
		}
		if err := s.CreateChunk(&c); err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
		pos += len(txt) + 1
	}
	return chunks
}

func TestWindowAround_middle(t *testing.T) {
	svc, s := setup(t)
	chunks := seed(t, s, "a.txt", 7)

	w, err := svc.WindowAround(chunks[3].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Segments) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(w.Segments))
	}
	if !w.HasPrev || !w.HasNext {
		t.Fatalf("expected HasPrev and HasNext")
	}
	if w.PrevChunk != chunks[0].ID || w.NextChunk != chunks[6].ID {
		t.Fatalf("prev/next chunk ids wrong: %s %s", w.PrevChunk, w.NextChunk)
	}
	if w.Segments[2].ChunkID != chunks[3].ID || !w.Segments[2].Focus {
		t.Fatalf("focus segment wrong: %+v", w.Segments[2])
	}
	if w.Text != "chunkB chunkC chunkD chunkE chunkF" {
		t.Fatalf("continuous text wrong: %q", w.Text)
	}
}

func TestWindowAround_edges(t *testing.T) {
	svc, s := setup(t)
	chunks := seed(t, s, "a.txt", 3)

	w, _ := svc.WindowAround(chunks[0].ID, 2)
	if w.HasPrev {
		t.Fatal("first chunk should not HasPrev")
	}
	if !w.HasNext {
		// only 3 chunks, radius 2 -> covers all, no next
		if len(w.Segments) != 3 {
			t.Fatalf("expected all 3 segments, got %d", len(w.Segments))
		}
	}
}

func TestWindowFrom_offset(t *testing.T) {
	svc, s := setup(t)
	chunks := seed(t, s, "a.txt", 5)
	// offset inside chunk index 2
	target := chunks[2].StartOffset + 1

	w, err := svc.WindowFrom("a.txt", target, 1)
	if err != nil {
		t.Fatal(err)
	}
	if w.FocusChunk != chunks[2].ID {
		t.Fatalf("expected focus on chunk 2, got %s", w.FocusChunk)
	}
	if !strings.Contains(w.Text, "chunkC") {
		t.Fatalf("window text missing focus chunk: %q", w.Text)
	}
}

func TestWindowAround_scrubsAcrossFilesIndependently(t *testing.T) {
	svc, s := setup(t)
	seed(t, s, "a.txt", 3)
	b := seed(t, s, "b.txt", 3)

	w, _ := svc.WindowAround(b[1].ID, 5)
	if w.SourceFile != "b.txt" || len(w.Segments) != 3 {
		t.Fatalf("window leaked across source files: %+v", w)
	}
}
