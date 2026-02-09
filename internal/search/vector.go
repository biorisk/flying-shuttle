package search

import (
	"context"
	"sort"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// VectorIndex is an in-memory brute-force vector search index.
// For the single-user writing tool scale, brute-force cosine similarity
// over a few thousand chunks is fast enough (<1ms).
type VectorIndex struct {
	embedder ingest.Embedder
	docs     []vecDoc
}

type vecDoc struct {
	id  string
	vec []float32
}

// NewVectorIndex creates an empty vector index backed by the given embedder.
func NewVectorIndex(embedder ingest.Embedder) *VectorIndex {
	return &VectorIndex{embedder: embedder}
}

// Add indexes a chunk by its ID and pre-computed embedding vector.
func (idx *VectorIndex) Add(id string, vec []float32) {
	idx.docs = append(idx.docs, vecDoc{id: id, vec: vec})
}

// Search embeds the query, then ranks all indexed documents by cosine
// similarity, returning up to limit results.
func (idx *VectorIndex) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if len(idx.docs) == 0 {
		return nil, nil
	}

	qVec, err := idx.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(idx.docs))
	for _, doc := range idx.docs {
		sim := ingest.CosineSimilarity(qVec, doc.vec)
		results = append(results, Result{ChunkID: doc.id, Score: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
