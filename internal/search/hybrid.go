package search

import (
	"context"
	"sort"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
)

// Result represents a single search hit with a relevance score.
type Result struct {
	ChunkID string  `json:"chunk_id"`
	Score   float64 `json:"score"`
}

// HybridIndex combines BM25 keyword search with vector semantic search
// using Reciprocal Rank Fusion (RRF) for score combination.
//
// When Embedder is nil, Search falls back to BM25-only results. This is the
// expected mode when embeddings are pre-computed offline and no live embedder
// is available at query time.
type HybridIndex struct {
	BM25     *BM25Index
	Vector   *VectorIndex
	Embedder ingest.Embedder // optional; nil → BM25-only mode

	// RRFk is the RRF constant (default 60). Higher values give more
	// weight to lower-ranked results.
	RRFk float64
}

// NewHybridIndex creates an empty hybrid index. Pass a nil embedder to enable
// BM25-only mode (vector search is skipped at query time).
func NewHybridIndex(embedder ingest.Embedder) *HybridIndex {
	return &HybridIndex{
		BM25:     NewBM25Index(),
		Vector:   NewVectorIndex(),
		Embedder: embedder,
		RRFk:     60,
	}
}

// IndexChunks populates both BM25 and vector indexes from a slice of chunks.
func (h *HybridIndex) IndexChunks(chunks []model.Chunk) {
	for _, c := range chunks {
		h.BM25.Add(c.ID, c.Content)
		if len(c.EmbeddingVec) > 0 {
			vec := ingest.BytesToFloat32s(c.EmbeddingVec)
			h.Vector.Add(c.ID, vec)
		}
	}
}

// IndexChunk adds a single chunk to both indexes.
func (h *HybridIndex) IndexChunk(c *model.Chunk) {
	h.BM25.Add(c.ID, c.Content)
	if len(c.EmbeddingVec) > 0 {
		vec := ingest.BytesToFloat32s(c.EmbeddingVec)
		h.Vector.Add(c.ID, vec)
	}
}

// Search performs hybrid retrieval: BM25 keyword search fused with vector
// semantic search via Reciprocal Rank Fusion. When no Embedder is configured,
// only BM25 results are returned. Returns up to limit results.
func (h *HybridIndex) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	// Get candidates from both sources. Fetch more than limit so RRF
	// has enough candidates to work with.
	candidateLimit := limit * 3
	if candidateLimit < 20 {
		candidateLimit = 20
	}

	bm25Results := h.BM25.Search(query, candidateLimit)

	var vecResults []Result
	if h.Embedder != nil {
		qVec, err := h.Embedder.Embed(ctx, query)
		if err != nil {
			return nil, err
		}
		vecResults = h.Vector.Search(qVec, candidateLimit)
	}

	fused := reciprocalRankFusionK(h.rrfk(), bm25Results, vecResults)

	if limit > 0 && len(fused) > limit {
		fused = fused[:limit]
	}
	return fused, nil
}

func (h *HybridIndex) rrfk() float64 {
	if h.RRFk > 0 {
		return h.RRFk
	}
	return 60
}

// reciprocalRankFusionK combines ranked result lists using RRF.
// score(d) = sum over lists of 1/(k + rank_in_list).
func reciprocalRankFusionK(k float64, lists ...[]Result) []Result {
	scores := make(map[string]float64)

	for _, list := range lists {
		for rank, r := range list {
			scores[r.ChunkID] += 1.0 / (k + float64(rank+1))
		}
	}

	results := make([]Result, 0, len(scores))
	for id, score := range scores {
		results = append(results, Result{ChunkID: id, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}
