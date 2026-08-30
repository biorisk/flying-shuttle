// Package indexer keeps the search index in sync with the store: it persists
// the index to disk incrementally so restarts are fast, and it backfills chunk
// embeddings in the background once an embedder is available.
package indexer

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/biorisk/flying-shuttle/internal/search"
)

// Snapshotter periodically flushes a HybridIndex to disk when it is dirty,
// plus once more on shutdown. Writes are atomic (temp file + rename), so a
// crash mid-flush leaves the previous snapshot intact.
type Snapshotter struct {
	idx      *search.HybridIndex
	bm25Path string
	hnswPath string
	interval time.Duration
}

// NewSnapshotter creates a Snapshotter. hnswPath may be empty to skip vector
// persistence.
func NewSnapshotter(idx *search.HybridIndex, bm25Path, hnswPath string, interval time.Duration) *Snapshotter {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Snapshotter{idx: idx, bm25Path: bm25Path, hnswPath: hnswPath, interval: interval}
}

// Run flushes on each interval tick while the index is dirty, and performs a
// final flush when ctx is cancelled. It blocks until ctx is done.
func (s *Snapshotter) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := s.Flush(); err != nil {
				log.Printf("indexer: final snapshot failed: %v", err)
			}
			return
		case <-t.C:
			if err := s.Flush(); err != nil {
				log.Printf("indexer: snapshot failed: %v", err)
			}
		}
	}
}

// Flush writes the index to disk if it has unsaved changes. It is safe to call
// concurrently with index writes: the dirty flag is cleared first, so any
// write racing the flush re-marks the index and is caught next time.
func (s *Snapshotter) Flush() error {
	if !s.idx.Dirty() {
		return nil
	}
	s.idx.ClearDirty()

	var errs []error
	if err := s.idx.SnapshotBM25(s.bm25Path); err != nil {
		s.idx.MarkDirty()
		errs = append(errs, err)
	}
	if s.hnswPath != "" {
		if err := s.idx.SnapshotVector(s.hnswPath); err != nil {
			s.idx.MarkDirty()
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
