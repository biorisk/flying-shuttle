package indexer

import (
	"log"
	"time"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
)

// LoadAndReconcile brings a fresh HybridIndex up to date at startup:
//
//  1. load the on-disk BM25 and vector snapshots, if present
//  2. add any chunks the store has that the BM25 index is missing
//     (everything, on first run — a full rebuild)
//  3. add any embedded chunks the vector index is missing
//
// After a clean shutdown steps 2–3 are no-ops, so boot stays fast regardless
// of corpus size.
func LoadAndReconcile(s store.Store, idx *search.HybridIndex, bm25Path, hnswPath string) error {
	start := time.Now()

	if ok, err := idx.LoadBM25(bm25Path); err != nil {
		log.Printf("indexer: BM25 snapshot unreadable (%v); rebuilding from store", err)
	} else if ok {
		log.Printf("indexer: loaded BM25 snapshot (%d docs)", idx.BM25.Len())
	}

	if hnswPath != "" {
		if ok, err := idx.LoadVector(hnswPath); err != nil {
			log.Printf("indexer: vector snapshot unreadable (%v); will rebuild from stored embeddings", err)
		} else if ok {
			log.Printf("indexer: loaded vector snapshot (%d vectors)", idx.Vector.Len())
		}
	}

	ids, err := s.ListChunkIDs()
	if err != nil {
		return err
	}

	var missing []string
	for _, id := range ids {
		if !idx.BM25.Has(id) {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		chunks, err := s.GetChunksByIDs(missing)
		if err != nil {
			return err
		}
		for i := range chunks {
			idx.IndexChunk(&chunks[i])
		}
		log.Printf("indexer: reconciled %d chunk(s) into the index in %s", len(missing), time.Since(start).Round(time.Millisecond))
	}

	// Reconcile the vector index against stored embeddings that aren't in the
	// graph yet (e.g. snapshot lost, or embeddings imported while offline).
	embIDs, err := s.ListChunkIDsWithEmbedding()
	if err != nil {
		return err
	}
	var vecMissing []string
	for _, id := range embIDs {
		if !idx.Vector.Has(id) {
			vecMissing = append(vecMissing, id)
		}
	}
	if len(vecMissing) > 0 {
		chunks, err := s.GetChunksByIDs(vecMissing)
		if err != nil {
			return err
		}
		for i := range chunks {
			c := &chunks[i]
			if len(c.EmbeddingVec) == 0 {
				continue
			}
			idx.SetChunkVector(c.ID, ingest.BytesToFloat32s(c.EmbeddingVec))
		}
		log.Printf("indexer: restored %d embedding(s) into the vector index", len(vecMissing))
	}

	return nil
}
