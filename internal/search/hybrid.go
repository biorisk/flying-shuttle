package search

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
)

// Result represents a single search hit with a relevance score.
type Result struct {
	ChunkID string  `json:"chunk_id"`
	Score   float64 `json:"score"`
	// BM25 and Vector are this hit's Reciprocal Rank Fusion contributions from
	// the keyword and semantic arms respectively; together they make up Score.
	// Both zero for results that didn't pass through hybrid fusion.
	BM25   float64 `json:"bm25"`
	Vector float64 `json:"vector"`
	// Passage is the rune span of the best-matching sub-chunk retrieval unit
	// within the chunk, when the passage arm contributed this hit. Zero span
	// otherwise. The evidence pane seeds its located window from it.
	Passage Span `json:"passage,omitempty"`
}

// MatchKind labels why a result matched, from its fusion provenance:
// "keyword" (BM25 only), "semantic" (vector only), or "hybrid" (both).
func (r Result) MatchKind() string {
	switch {
	case r.BM25 > 0 && r.Vector > 0:
		return "hybrid"
	case r.Vector > 0:
		return "semantic"
	default:
		return "keyword"
	}
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
	Passages *BM25Index      // sub-chunk retrieval units; not snapshotted
	Embedder ingest.Embedder // optional; nil → BM25-only mode

	// RRFk is the RRF constant (default 60). Higher values give more
	// weight to lower-ranked results.
	RRFk float64

	// dirty is set whenever the in-memory index diverges from its last
	// on-disk snapshot. The Snapshotter clears it after a successful flush.
	dirty atomic.Bool
}

// MarkDirty flags the index as having unsaved changes.
func (h *HybridIndex) MarkDirty() { h.dirty.Store(true) }

// Dirty reports whether the index has changed since the last snapshot.
func (h *HybridIndex) Dirty() bool { return h.dirty.Load() }

// ClearDirty resets the dirty flag. Call it immediately before a flush so
// concurrent writes during the flush re-mark the index dirty.
func (h *HybridIndex) ClearDirty() { h.dirty.Store(false) }

// NewHybridIndex creates an empty hybrid index. Pass a nil embedder to enable
// BM25-only mode (vector search is skipped at query time).
func NewHybridIndex(embedder ingest.Embedder) *HybridIndex {
	return &HybridIndex{
		BM25:     NewBM25Index(),
		Vector:   NewVectorIndex(),
		Passages: NewBM25Index(),
		Embedder: embedder,
		RRFk:     60,
	}
}

// IndexChunks populates both BM25 and vector indexes from a slice of chunks.
func (h *HybridIndex) IndexChunks(chunks []model.Chunk) {
	for i := range chunks {
		h.IndexChunk(&chunks[i])
	}
}

// IndexChunk adds a single chunk to the keyword, passage, and vector indexes.
func (h *HybridIndex) IndexChunk(c *model.Chunk) {
	h.BM25.Add(c.ID, c.Content)
	h.indexPassages(c.ID, c.Content)
	if len(c.EmbeddingVec) > 0 {
		vec := ingest.BytesToFloat32s(c.EmbeddingVec)
		h.Vector.Add(c.ID, vec)
	}
	h.dirty.Store(true)
}

func (h *HybridIndex) indexPassages(chunkID, content string) {
	if h.Passages == nil {
		return
	}
	for _, p := range chunkPassages(content) {
		h.Passages.Add(PassageID(chunkID, p.Span), p.Text)
	}
}

// RebuildPassages repopulates the passage sub-index from scratch. The passage
// index is not snapshotted, so startup reconciliation calls this with every
// stored chunk.
func (h *HybridIndex) RebuildPassages(chunks []model.Chunk) {
	if h.Passages == nil {
		h.Passages = NewBM25Index()
	}
	for i := range chunks {
		h.indexPassages(chunks[i].ID, chunks[i].Content)
	}
}

// SetChunkVector adds or replaces a chunk's embedding in the vector index.
func (h *HybridIndex) SetChunkVector(id string, vec []float32) {
	h.Vector.Add(id, vec)
	h.dirty.Store(true)
}

// Mode selects which retrieval arm(s) HybridIndex.SearchMode draws from.
type Mode string

const (
	// ModeHybrid fuses BM25 keyword, passage, and vector semantic results via
	// RRF — the default, and the only mode Search (below) ever runs.
	ModeHybrid Mode = "hybrid"
	// ModeKeyword restricts retrieval to the BM25 keyword and passage arms;
	// no embedder call is made.
	ModeKeyword Mode = "keyword"
	// ModeSemantic restricts retrieval to the vector arm. Empty (not an
	// error) when no Embedder is configured or nothing has been embedded yet.
	ModeSemantic Mode = "semantic"
)

// normalize maps an unrecognized or empty Mode to ModeHybrid, so callers that
// pass through an untrusted string (e.g. a query param) degrade safely.
func (m Mode) normalize() Mode {
	switch m {
	case ModeKeyword, ModeSemantic:
		return m
	default:
		return ModeHybrid
	}
}

// Search performs hybrid retrieval: BM25 keyword search fused with vector
// semantic search via Reciprocal Rank Fusion. When no Embedder is configured,
// only BM25 results are returned. Returns up to limit results. Equivalent to
// SearchMode(ctx, query, limit, ModeHybrid).
func (h *HybridIndex) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return h.SearchMode(ctx, query, limit, ModeHybrid)
}

// MaxVectorPoolSize bounds how many HNSW neighbors the vector arm ever pulls
// when SearchMode is asked for the full ranked pool (limit <= 0). Vector
// search is approximate top-k, not exhaustive, so unlike BM25/passage there
// is no true "every match" for it — this is the practical ceiling on how
// deep a semantic-mode caller can page.
const MaxVectorPoolSize = 500

// SearchMode is Search with an explicit retrieval mode: ModeHybrid (default),
// ModeKeyword (BM25 + passage only, no embedder call), or ModeSemantic
// (vector only). An unrecognized mode is treated as ModeHybrid.
//
// limit > 0 behaves as before: each arm is fetched to roughly 3x limit (min
// 20) before fusing, and the fused result is truncated to limit. limit <= 0
// returns the FULL ranked pool, not truncated: BM25 and passage are
// exhaustive (every term-matching doc, no cap — cheap, since both already
// score every match internally before any truncation), and vector is capped
// at MaxVectorPoolSize since it has no exhaustive equivalent. Callers that
// want to paginate (e.g. the evidence pane) should request the full pool
// once and slice it themselves, rather than re-querying per page.
func (h *HybridIndex) SearchMode(ctx context.Context, query string, limit int, mode Mode) ([]Result, error) {
	full := limit <= 0

	// Get candidates from both sources. Fetch more than limit so RRF
	// has enough candidates to work with (or everything, for a full pool).
	candidateLimit := limit * 3
	if candidateLimit < 20 {
		candidateLimit = 20
	}
	bm25Limit, passageLimit, vecLimit := candidateLimit, candidateLimit, candidateLimit
	if full {
		bm25Limit, passageLimit = 0, 0 // BM25Index.Search(_, 0) = every match, unbounded
		vecLimit = MaxVectorPoolSize
	}

	mode = mode.normalize()

	var bm25Results, passageResults []Result
	if mode != ModeSemantic {
		bm25Results = h.BM25.Search(query, bm25Limit)

		// Passage arm: match small retrieval units, then roll each chunk's best
		// passage up into a chunk-level result carrying that passage's span.
		if h.Passages != nil && h.Passages.Len() > 0 {
			seen := make(map[string]bool)
			passageScanLimit := passageLimit * 3
			if full {
				passageScanLimit = 0
			}
			for _, pr := range h.Passages.Search(query, passageScanLimit) {
				cid, span, ok := SplitPassageID(pr.ChunkID)
				if !ok || seen[cid] {
					continue
				}
				seen[cid] = true
				passageResults = append(passageResults, Result{ChunkID: cid, Score: pr.Score, Passage: span})
				if !full && len(passageResults) >= passageLimit {
					break
				}
			}
		}
	}

	var vecResults []Result
	if mode != ModeKeyword && h.Embedder != nil && h.Vector.Len() > 0 {
		qVec, err := ingest.EmbedQueryOr(ctx, h.Embedder, query)
		if err != nil {
			// Embedder unavailable (e.g. still warming up) — degrade to
			// whatever keyword/passage results we already have rather than
			// failing the whole query. In ModeSemantic that's none, so the
			// caller correctly sees an empty result set.
			fused := fuseArms(h.rrfk(), bm25Results, passageResults, nil)
			if !full && len(fused) > limit {
				fused = fused[:limit]
			}
			return fused, nil
		}
		vecResults = h.Vector.Search(qVec, vecLimit)
	}

	fused := fuseArms(h.rrfk(), bm25Results, passageResults, vecResults)

	if !full && len(fused) > limit {
		fused = fused[:limit]
	}
	return fused, nil
}

// LoadBM25 restores the BM25 index from a snapshot file. A missing file is
// not an error — the index is simply left empty for the caller to rebuild.
func (h *HybridIndex) LoadBM25(path string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := h.BM25.Load(bufio.NewReader(f)); err != nil {
		return false, err
	}
	return true, nil
}

// LoadVector restores the HNSW vector index from a snapshot file. A missing
// file is not an error.
func (h *HybridIndex) LoadVector(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	if err := h.Vector.Load(path); err != nil {
		return false, err
	}
	return true, nil
}

// SnapshotBM25 atomically writes the BM25 index to path.
func (h *HybridIndex) SnapshotBM25(path string) error {
	return atomicWrite(path, h.BM25.Save)
}

// SnapshotVector atomically writes the HNSW vector index to path.
func (h *HybridIndex) SnapshotVector(path string) error {
	return atomicWrite(path, h.Vector.Export)
}

// atomicWrite writes via a temp file in the same directory, then renames it
// over path so readers never see a half-written index.
func atomicWrite(path string, encode func(io.Writer) error) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	bw := bufio.NewWriter(tmp)
	if err = encode(bw); err != nil {
		tmp.Close()
		return err
	}
	if err = bw.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (h *HybridIndex) rrfk() float64 {
	if h.RRFk > 0 {
		return h.RRFk
	}
	return 60
}

// fuseArms runs Reciprocal Rank Fusion over the keyword (bm25), passage, and
// semantic (vec) result lists. The passage arm counts toward keyword
// provenance and contributes its span; each arm's contribution is recorded on
// the fused Result so the UI can explain why a chunk matched.
func fuseArms(k float64, bm25, passage, vec []Result) []Result {
	type acc struct {
		bm, vc  float64
		passage Span
	}
	scores := make(map[string]*acc)
	get := func(id string) *acc {
		a := scores[id]
		if a == nil {
			a = &acc{}
			scores[id] = a
		}
		return a
	}
	for rank, r := range bm25 {
		get(r.ChunkID).bm += 1.0 / (k + float64(rank+1))
	}
	for rank, r := range passage {
		a := get(r.ChunkID)
		a.bm += 1.0 / (k + float64(rank+1))
		a.passage = r.Passage
	}
	for rank, r := range vec {
		get(r.ChunkID).vc += 1.0 / (k + float64(rank+1))
	}

	results := make([]Result, 0, len(scores))
	for id, a := range scores {
		results = append(results, Result{
			ChunkID: id, Score: a.bm + a.vc,
			BM25: a.bm, Vector: a.vc, Passage: a.passage,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results
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
