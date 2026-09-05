package corpus

import (
	"path/filepath"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

func TestOpenMigrateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")

	s, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "iv.txt", Content: "hello", EndOffset: 5}
	if err := s.CreateChunk(c); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("embed_model", "test-768"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen read-only: migrations skipped, data still readable.
	ro, err := Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	got, err := ro.GetChunk(c.ID)
	if err != nil || got.Content != "hello" {
		t.Fatalf("GetChunk after reopen: %+v err=%v", got, err)
	}
	if v, _ := ro.GetMeta("embed_model"); v != "test-768" {
		t.Fatalf("meta lost across reopen: %q", v)
	}
}
