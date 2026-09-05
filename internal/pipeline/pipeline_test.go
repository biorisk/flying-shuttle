package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/corpus"
)

func newIngester(t *testing.T) (*Ingester, corpus.Store) {
	t.Helper()
	s, err := corpus.Open(filepath.Join(t.TempDir(), "c.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &Ingester{Store: s, UploadDir: t.TempDir()}, s
}

func waitForChunks(t *testing.T, s corpus.Store, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cs, _ := s.ListChunks()
		if len(cs) >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	cs, _ := s.ListChunks()
	t.Fatalf("want >=%d chunks, got %d", want, len(cs))
}

func TestIngestPath_directory(t *testing.T) {
	in, s := newIngester(t)
	src := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "Para one about fear.\n\nPara two about resolve.")
	write("b.md", "# Heading\n\nA second transcript paragraph here.")
	write("notes.pdf", "%PDF-1.4 binary junk")
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("sub", "c.text"), "Nested transcript paragraph.")

	accepted, skipped, err := in.IngestPath(src)
	if err != nil {
		t.Fatalf("IngestPath: %v", err)
	}
	if len(accepted) != 3 {
		t.Fatalf("want 3 accepted, got %d", len(accepted))
	}
	if len(skipped) != 1 {
		t.Fatalf("want 1 skipped (the .pdf), got %d: %v", len(skipped), skipped)
	}
	waitForChunks(t, s, 3)

	ups, total, _ := s.ListUploadsPage(0, 0)
	if total != 3 || len(ups) != 3 {
		t.Fatalf("want 3 uploads, got total=%d rows=%d", total, len(ups))
	}
}

func TestIngestPath_singleFile(t *testing.T) {
	in, s := newIngester(t)
	p := filepath.Join(t.TempDir(), "interview.txt")
	if err := os.WriteFile(p, []byte("One paragraph.\n\nTwo paragraph."), 0o644); err != nil {
		t.Fatal(err)
	}
	accepted, _, err := in.IngestPath(p)
	if err != nil {
		t.Fatalf("IngestPath: %v", err)
	}
	if len(accepted) != 1 || accepted[0].Filename != "interview.txt" {
		t.Fatalf("unexpected accepted: %+v", accepted)
	}
	waitForChunks(t, s, 1)
}

func TestIngestPath_missing(t *testing.T) {
	in, _ := newIngester(t)
	if _, _, err := in.IngestPath(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("want error for a missing path")
	}
}

func TestIngestPath_nonTranscriptFile(t *testing.T) {
	in, _ := newIngester(t)
	p := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := in.IngestPath(p); err == nil {
		t.Fatal("want error for a non-transcript file")
	}
}
