package atlas

import (
	"context"
	"sort"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// RegionHit is a ranked region from a RegionIndex query.
type RegionHit struct {
	RegionID string
	Score    float64 // cosine similarity to the query vector
}

// RegionIndex ranks regions by cosine similarity of their digest embeddings to
// a query vector. It is a flat scan — builds have hundreds of regions, not
// millions — and is entirely separate from the chunk-level search.HybridIndex.
type RegionIndex struct {
	ids  []string
	vecs [][]float32
}

// NewRegionIndex returns an empty index.
func NewRegionIndex() *RegionIndex { return &RegionIndex{} }

// LoadRegionIndex builds an index from a build's regions that have a digest
// embedding. Regions without DigestVec (Phase C not run, or the embedder was
// unavailable) are simply absent from the index.
func LoadRegionIndex(b *Build) *RegionIndex {
	ix := NewRegionIndex()
	if b == nil {
		return ix
	}
	for i := range b.Regions {
		if len(b.Regions[i].DigestVec) > 0 {
			ix.Add(b.Regions[i].ID, b.Regions[i].DigestVec)
		}
	}
	return ix
}

// Add registers a region's digest vector. A later Add for the same id appends
// a duplicate; callers build the index once per Atlas build.
func (ix *RegionIndex) Add(regionID string, digestVec []float32) {
	if len(digestVec) == 0 {
		return
	}
	ix.ids = append(ix.ids, regionID)
	ix.vecs = append(ix.vecs, digestVec)
}

// Len reports how many regions are indexed.
func (ix *RegionIndex) Len() int { return len(ix.ids) }

// Rank returns up to limit regions most similar to query, highest score first.
// Ties break on region id for determinism. A zero/empty query or empty index
// yields nil.
func (ix *RegionIndex) Rank(query []float32, limit int) []RegionHit {
	if len(query) == 0 || len(ix.ids) == 0 {
		return nil
	}
	hits := make([]RegionHit, 0, len(ix.ids))
	for i, v := range ix.vecs {
		hits = append(hits, RegionHit{
			RegionID: ix.ids[i],
			Score:    ingest.CosineSimilarity(query, v),
		})
	}
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		return hits[a].RegionID < hits[b].RegionID
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// digestText is the string embedded to represent a region for search.
func digestText(d Digest) string {
	parts := make([]string, 0, 3)
	if d.Title != "" {
		parts = append(parts, d.Title)
	}
	if d.Abstract != "" {
		parts = append(parts, d.Abstract)
	}
	if len(d.Keywords) > 0 {
		parts = append(parts, strings.Join(d.Keywords, " "))
	}
	return strings.Join(parts, "\n")
}

// EmbedDigests fills DigestVec for every region with a non-empty digest by
// batch-embedding digestText through emb. On ingest.ErrEmbedderNotReady it
// returns that error with DigestVec left unset — the Atlas stays usable
// (browse works; only digest search / bullet affinity are unavailable).
func EmbedDigests(ctx context.Context, emb ingest.Embedder, regions []Region) error {
	if emb == nil {
		return ingest.ErrEmbedderNotReady
	}
	idx := make([]int, 0, len(regions))
	texts := make([]string, 0, len(regions))
	for i := range regions {
		t := digestText(regions[i].Digest)
		if t == "" {
			continue
		}
		idx = append(idx, i)
		texts = append(texts, t)
	}
	if len(texts) == 0 {
		return nil
	}
	vecs, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		return err
	}
	for k, i := range idx {
		if k < len(vecs) {
			regions[i].DigestVec = vecs[k]
		}
	}
	return nil
}
