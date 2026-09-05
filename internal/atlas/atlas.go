// Package atlas builds and stores the Source Atlas: a derived, disposable
// NETWORK over the transcript corpus that helps the author find source
// material to bring into the outline.
//
// The Atlas is not the authored document. Keep the vocabulary separate:
//
//	Outline (internal/outline, internal/dag)   Source Atlas (this package)
//	  node   — a bullet in the document           region — a cluster of chunks
//	  edge   — linear / branch / jump link        link   — region↔region similarity
//	  thread, branch, snapshot, evidence          digest — a region's summary
//
// The Atlas is a flat, undirected, weighted graph: regions connected by links,
// with no root, no hierarchy, no containment. It is rebuilt from scratch (one
// build kept at a time) and is never versioned, snapshotted, exported, or
// recovered. The only bridge to the outline is the existing evidence flow: the
// author attaches a region's member chunk as evidence under a node.
package atlas

import (
	"errors"
	"strings"
	"time"
)

// ErrNoBuild is returned when no ready Atlas build exists yet.
var ErrNoBuild = errors.New("atlas: no build")

// ErrReadOnly is returned when a rebuild is attempted on a read-only corpus
// session (the writer lock is held by another project).
var ErrReadOnly = errors.New("atlas: corpus is read-only")

// BuildStatus tracks a build's lifecycle.
type BuildStatus string

const (
	StatusBuilding BuildStatus = "building"
	StatusReady    BuildStatus = "ready"
	StatusFailed   BuildStatus = "failed"
)

// Build is one construction of the Source Atlas. Only one build is kept at a
// time; rebuilding replaces it wholesale.
type Build struct {
	ID         string
	CreatedAt  time.Time
	Status     BuildStatus
	ChunkCount int
	Params     string // opaque JSON blob of the build parameters
	Error      string

	// Populated by full reads (GetBuild / CurrentBuild).
	Regions []Region
	Links   []Link

	// Transcripts holds one digest per source file, computed from that
	// file's own full text (not sampled from a region). Persisted and
	// reloaded like Regions/Links; the network graph view still falls back
	// to the filename if a transcript somehow has no digest.
	Transcripts []TranscriptDigest
}

// TranscriptDigest is one source file's digest for the network graph's
// top-level transcript nodes, built from the full sequence of that file's own
// chunks — never a region-based sample of chunks from elsewhere.
type TranscriptDigest struct {
	SourceFile string
	ChunkCount int
	Digest     Digest
}

// ChunkLabel is a short label for one chunk, used as its node label in the
// transcript drill-down view. Persisted per chunk (not per build) — see
// migrations/009_atlas_chunk_label.sql. A row with Source "llm:<model>" is
// final and never recomputed. A "head" row (LLM was down or mangled that
// line) is a placeholder the next build re-attempts.
type ChunkLabel struct {
	ChunkID string
	Label   string
	Source  string // "llm:<model>" (final) | "head" (retried next build)
}

// Region is a cluster of embedding-near transcript chunks. It is NOT an
// outline node.
type Region struct {
	ID         string
	BuildID    string
	Centroid   []float32
	ChunkCount int
	Digest     Digest
	DigestVec  []float32 // nil until embedded in Phase C

	// Populated by full reads.
	Members []Member
}

// Digest is the summary attached to a region.
type Digest struct {
	Title    string
	Abstract string
	Keywords []string
	Source   string // "llm:<model>" | "extractive" | ""
}

// Member is a chunk's membership in a region.
type Member struct {
	ChunkID  string
	Distance float64  // cosine distance to the region centroid
	Keywords []string // extractive tags, the chunk-node label in the graph view
}

// Link is an undirected, weighted similarity connection between two regions.
// It is NOT an outline edge. RegionA is always the lexically smaller id.
type Link struct {
	RegionA string
	RegionB string
	Weight  float64 // centroid cosine similarity
}

// NewLink returns a Link with the region ids in canonical (RegionA < RegionB)
// order. It returns ok=false for a self-link.
func NewLink(r1, r2 string, weight float64) (Link, bool) {
	if r1 == r2 {
		return Link{}, false
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	return Link{RegionA: r1, RegionB: r2, Weight: weight}, true
}

// joinKeywords / splitKeywords are the on-disk encoding for keyword lists
// (newline-joined, matching the SQL columns).
func joinKeywords(kw []string) string { return strings.Join(kw, "\n") }

func splitKeywords(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
