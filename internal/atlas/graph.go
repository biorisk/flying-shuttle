package atlas

import (
	"math"
	"sort"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// This file supports the network GRAPH VIEW only (source_atlas_plan.md §6,
// revised twice). The view's top-level nodes are TRANSCRIPTS (source files),
// not chunks and not regions — a disjoint, already-meaningful partition that
// needs no clustering. Tapping a transcript drills into its own chunks,
// connected only by their sequence in that transcript (chunk[i]-chunk[i+1]),
// never by embedding similarity — that drill-down is built straight from
// Store.ListChunksBySourceFile and never touches this file.
//
// A region still becomes a TAG here, but at file granularity: TagFiles tags
// each transcript with whichever region(s) its chunks are concentrated in,
// rendered client-side as an overlapping Bubble Sets hull. BuildChunkEdges'
// chunk-level similarity graph is never sent to the client directly anymore
// — it's purely the substrate BuildTranscriptEdges aggregates into
// transcript-to-transcript edges. None of this touches Region/Member/Link, or
// the hard k-means clustering they come from — search ranking and digests are
// unaffected.

// RegionTag is one region a graph node (a transcript, in the network view) is
// tagged with, for the Bubble Sets overlay.
type RegionTag struct {
	RegionID string
	Weight   float64 // share of the transcript's chunks primarily in that region, 0..1
}

// FileTagParams controls how many extra (non-primary) region tags a
// transcript picks up in TagFiles.
type FileTagParams struct {
	// MinShare: a region must cover at least this fraction of a transcript's
	// chunks to tag it at all (beyond the guaranteed primary tag). Default
	// 0.15.
	MinShare float64
	// Margin: a transcript also tags onto any region within Margin share of
	// its primary region's share. Default 0.1.
	Margin float64
	// MaxExtra caps how many additional tags a transcript can carry beyond
	// its primary (plurality) region. Default 2.
	MaxExtra int
}

func (p FileTagParams) withDefaults() FileTagParams {
	if p.MinShare <= 0 {
		p.MinShare = 0.15
	}
	if p.Margin <= 0 {
		p.Margin = 0.1
	}
	if p.MaxExtra <= 0 {
		p.MaxExtra = 2
	}
	return p
}

// TagFiles assigns every transcript (source file) one or more region tags for
// the network graph view. A transcript's primary tag is whichever region has
// a plurality of its chunks; it additionally tags onto any other region
// covering at least MinShare of its chunks and within Margin share of the
// primary, up to MaxExtra extras. fileOfChunk maps chunk id -> source file
// for every chunk in regions' Members.
func TagFiles(regions []Region, fileOfChunk map[string]string, params FileTagParams) map[string][]RegionTag {
	p := params.withDefaults()

	counts := map[string]map[string]int{} // file -> regionID -> chunk count
	totals := map[string]int{}            // file -> total tagged chunks
	for i := range regions {
		for _, m := range regions[i].Members {
			file := fileOfChunk[m.ChunkID]
			if file == "" {
				continue
			}
			if counts[file] == nil {
				counts[file] = map[string]int{}
			}
			counts[file][regions[i].ID]++
			totals[file]++
		}
	}

	type cand struct {
		id    string
		share float64
	}

	out := make(map[string][]RegionTag, len(counts))
	for file, byRegion := range counts {
		total := totals[file]
		cands := make([]cand, 0, len(byRegion))
		for regionID, n := range byRegion {
			cands = append(cands, cand{regionID, float64(n) / float64(total)})
		}
		sort.Slice(cands, func(a, b int) bool {
			if cands[a].share != cands[b].share {
				return cands[a].share > cands[b].share
			}
			return cands[a].id < cands[b].id
		})

		primary := cands[0]
		tags := []RegionTag{{RegionID: primary.id, Weight: primary.share}}
		for _, c := range cands[1:] {
			if len(tags)-1 >= p.MaxExtra {
				break
			}
			if c.share < p.MinShare || c.share < primary.share-p.Margin {
				break // cands is sorted descending, so nothing further qualifies
			}
			tags = append(tags, RegionTag{RegionID: c.id, Weight: c.share})
		}
		out[file] = tags
	}
	return out
}

// ChunkEdge is a similarity edge between two chunks. It is an internal
// aggregation input for BuildTranscriptEdges — the network graph view never
// renders chunk-to-chunk edges directly (see the file doc comment above).
type ChunkEdge struct {
	A, B   string
	Weight float64
}

// GraphEdgeParams controls the chunk-level similarity graph built by
// BuildChunkEdges.
type GraphEdgeParams struct {
	K         int     // nearest neighbours kept per chunk; default 6
	MinWeight float64 // drop links below this cosine similarity; default 0.35
	// KeepTopFraction, after the per-node top-K pass above, additionally
	// keeps only the globally strongest fraction of the surviving edges by
	// weight (e.g. 0.25 keeps the top quarter, dropping the rest). This is
	// the knob for "too many edges merge everything into one clump" — the
	// per-node K bounds degree, but on a large, densely-embedded corpus the
	// union of everyone's top-K can still be dense enough to read as one
	// blob. Default 1 (no additional trim).
	KeepTopFraction float64
}

func (p GraphEdgeParams) withDefaults() GraphEdgeParams {
	if p.K <= 0 {
		p.K = 6
	}
	if p.MinWeight == 0 {
		p.MinWeight = 0.35
	}
	if p.KeepTopFraction <= 0 || p.KeepTopFraction > 1 {
		p.KeepTopFraction = 1
	}
	return p
}

// BuildChunkEdges connects chunks into a sparse similarity graph: each chunk
// keeps a link to its K nearest other chunks (cosine over embeddings) above
// MinWeight. A link kept from either endpoint's neighbour list survives, so
// degree can exceed K — the same shape as BuildLinks, but at chunk
// granularity, and deliberately sparse (top-K per node, not every pair above
// a flat threshold). Its output is never rendered directly; it's the
// substrate BuildTranscriptEdges aggregates into transcript-to-transcript
// edges for the graph view.
//
// ids[i] is the chunk id for vectors[i]; entries with a nil/empty vector are
// skipped. Cost is O(n^2) in chunk count, same tradeoff as BuildLinks — fine
// into the low thousands for an offline/background build.
func BuildChunkEdges(ids []string, vectors [][]float32, params GraphEdgeParams) []ChunkEdge {
	p := params.withDefaults()

	var present []int
	for i, v := range vectors {
		if len(v) > 0 {
			present = append(present, i)
		}
	}
	n := len(present)
	if n < 2 {
		return nil
	}

	type cand struct {
		j int
		w float64
	}
	neighbours := make([][]cand, n)
	sims := make(map[[2]int]float64)
	for a := 0; a < n; a++ {
		for c := a + 1; c < n; c++ {
			i, j := present[a], present[c]
			w := ingest.CosineSimilarity(vectors[i], vectors[j])
			if w < p.MinWeight {
				continue
			}
			sims[[2]int{a, c}] = w
			neighbours[a] = append(neighbours[a], cand{c, w})
			neighbours[c] = append(neighbours[c], cand{a, w})
		}
	}

	keep := make(map[[2]int]bool)
	for a := range neighbours {
		ns := neighbours[a]
		sort.Slice(ns, func(x, y int) bool {
			if ns[x].w != ns[y].w {
				return ns[x].w > ns[y].w
			}
			return ids[present[ns[x].j]] < ids[present[ns[y].j]]
		})
		if len(ns) > p.K {
			ns = ns[:p.K]
		}
		for _, c := range ns {
			x, y := a, c.j
			if x > y {
				x, y = y, x
			}
			keep[[2]int{x, y}] = true
		}
	}

	edges := make([]ChunkEdge, 0, len(keep))
	for pair := range keep {
		a, b := ids[present[pair[0]]], ids[present[pair[1]]]
		w := sims[pair]
		if a > b {
			a, b = b, a
		}
		edges = append(edges, ChunkEdge{A: a, B: b, Weight: w})
	}
	sort.Slice(edges, func(x, y int) bool {
		if edges[x].Weight != edges[y].Weight {
			return edges[x].Weight > edges[y].Weight
		}
		if edges[x].A != edges[y].A {
			return edges[x].A < edges[y].A
		}
		return edges[x].B < edges[y].B
	})
	if p.KeepTopFraction < 1 {
		keepN := int(math.Ceil(float64(len(edges)) * p.KeepTopFraction))
		edges = edges[:keepN]
	}
	return edges
}

// TranscriptEdge is a similarity edge between two transcripts (source files)
// in the network graph's top-level view.
type TranscriptEdge struct {
	A, B   string
	Weight float64
}

// TranscriptEdgeParams controls the transcript-level graph built by
// BuildTranscriptEdges.
type TranscriptEdgeParams struct {
	K int // strongest edges kept per transcript; default 4
}

func (p TranscriptEdgeParams) withDefaults() TranscriptEdgeParams {
	if p.K <= 0 {
		p.K = 4
	}
	return p
}

// BuildTranscriptEdges aggregates a chunk-level similarity graph (from
// BuildChunkEdges) up to transcript granularity: two transcripts' edge weight
// is the MAX chunk-chunk weight crossing between them (their strongest single
// link, not a sum or average — one very related passage is enough to connect
// two files even if most of each is unrelated), and each transcript keeps
// only its K strongest cross-transcript edges. A link kept from either
// endpoint's side survives, so degree can exceed K — same keep-if-either-end-
// wants-it semantics as BuildLinks and BuildChunkEdges.
//
// fileOfChunk maps a chunk id (either endpoint of chunkEdges) to its source
// file; chunk edges with an unknown or equal-file endpoint are ignored (only
// cross-transcript edges are aggregated — a transcript's internal similarity
// isn't part of this graph).
func BuildTranscriptEdges(chunkEdges []ChunkEdge, fileOfChunk map[string]string, params TranscriptEdgeParams) []TranscriptEdge {
	p := params.withDefaults()

	maxW := map[[2]string]float64{}
	for _, e := range chunkEdges {
		fa, fb := fileOfChunk[e.A], fileOfChunk[e.B]
		if fa == "" || fb == "" || fa == fb {
			continue
		}
		if fa > fb {
			fa, fb = fb, fa
		}
		key := [2]string{fa, fb}
		if e.Weight > maxW[key] {
			maxW[key] = e.Weight
		}
	}
	if len(maxW) == 0 {
		return nil
	}

	type cand struct {
		other string
		w     float64
	}
	neighbours := map[string][]cand{}
	for pair, w := range maxW {
		neighbours[pair[0]] = append(neighbours[pair[0]], cand{pair[1], w})
		neighbours[pair[1]] = append(neighbours[pair[1]], cand{pair[0], w})
	}

	keep := map[[2]string]bool{}
	for file, ns := range neighbours {
		sort.Slice(ns, func(x, y int) bool {
			if ns[x].w != ns[y].w {
				return ns[x].w > ns[y].w
			}
			return ns[x].other < ns[y].other
		})
		if len(ns) > p.K {
			ns = ns[:p.K]
		}
		for _, c := range ns {
			a, b := file, c.other
			if a > b {
				a, b = b, a
			}
			keep[[2]string{a, b}] = true
		}
	}

	edges := make([]TranscriptEdge, 0, len(keep))
	for pair := range keep {
		edges = append(edges, TranscriptEdge{A: pair[0], B: pair[1], Weight: maxW[pair]})
	}
	sort.Slice(edges, func(x, y int) bool {
		if edges[x].Weight != edges[y].Weight {
			return edges[x].Weight > edges[y].Weight
		}
		if edges[x].A != edges[y].A {
			return edges[x].A < edges[y].A
		}
		return edges[x].B < edges[y].B
	})
	return edges
}
