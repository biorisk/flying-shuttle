// Package storetest provides test helpers that build a document store and a
// corpus store together. After the corpus/project split the two live in
// separate databases; for tests that exercise both halves (attach evidence,
// stitch, atlas) this opens one SQLite file, applies both migration sets to
// it, and hands back a store of each kind over that file.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/doc"
)

// Pair is a document store and a corpus store backed by the same on-disk
// SQLite file (a test convenience — production keeps them in separate files).
type Pair struct {
	Doc    doc.Store
	Corpus corpus.Store
}

// New opens a fresh combined store under t.TempDir() and registers cleanup.
func New(t testing.TB) Pair {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.db")

	d, err := doc.Open(path)
	if err != nil {
		t.Fatalf("open doc store: %v", err)
	}
	c, err := corpus.Open(path, false)
	if err != nil {
		d.Close()
		t.Fatalf("open corpus store: %v", err)
	}
	t.Cleanup(func() {
		c.Close()
		d.Close()
	})
	return Pair{Doc: d, Corpus: c}
}
