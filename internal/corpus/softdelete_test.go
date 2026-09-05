package corpus

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

func TestSoftDelete_hidesFromReadsButResolvable(t *testing.T) {
	s := newCorpus(t)
	c := &model.Chunk{ID: uuid.NewString(), SourceFile: "iv.txt", Content: "first take", EndOffset: 10}
	if err := s.CreateChunk(c); err != nil {
		t.Fatal(err)
	}

	n, err := s.SoftDeleteChunksBySourceFile("iv.txt")
	if err != nil || n != 1 {
		t.Fatalf("SoftDelete = %d, %v", n, err)
	}

	// Hidden from normal reads.
	if _, err := s.GetChunk(c.ID); err != ErrNotFound {
		t.Fatalf("GetChunk after soft-delete: %v", err)
	}
	if all, _ := s.ListChunks(); len(all) != 0 {
		t.Fatalf("ListChunks still returns %d", len(all))
	}

	// Still resolvable for the doctor, flagged deleted.
	found, deleted, err := s.ResolveChunk(c.ID)
	if err != nil || !found || !deleted {
		t.Fatalf("ResolveChunk = found:%v deleted:%v err:%v", found, deleted, err)
	}
	content, del, ok, _ := s.ChunkContentAnyState(c.ID)
	if !ok || !del || content != "first take" {
		t.Fatalf("ChunkContentAnyState = %q del:%v ok:%v", content, del, ok)
	}

	// A re-ingest writes a fresh row with a new id, untouched by the delete.
	c2 := &model.Chunk{ID: uuid.NewString(), SourceFile: "iv.txt", Content: "corrected take", EndOffset: 14}
	if err := s.CreateChunk(c2); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.ListChunks(); len(all) != 1 || all[0].ID != c2.ID {
		t.Fatalf("after re-ingest, live chunks = %+v", all)
	}
	if found, _, _ := s.ResolveChunk(uuid.NewString()); found {
		t.Fatal("a random id should not resolve")
	}
}
