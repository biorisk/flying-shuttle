// Package viewmodel holds the plain data structs passed from web handlers into
// templ components. It is a leaf package (imports nothing from internal/web or
// internal/web/components) so both sides can depend on it without a cycle.
package viewmodel

// Candidate is one ranked supporting passage surfaced for a bullet.
type Candidate struct {
	ChunkID    string
	SourceFile string
	Speaker    string
	// Snippet is the plain-text display passage: the located window (with
	// leading/trailing "…" when clipped) or, when no query term was located,
	// the chunk head.
	Snippet string
	// Segments is Snippet split into verbatim and highlighted (<mark>) runs.
	// Empty when there is nothing to highlight — render Snippet directly.
	Segments []SnippetSeg
	// Full is the whole chunk split into verbatim and <mark> runs, shown when
	// the card is expanded. Nil when the snippet already covers the chunk or
	// when FullSentences is used instead.
	Full []SnippetSeg
	// FullSentences is the expanded view broken into sentences, each carrying
	// a 0..1 relevance score for light→dark shading. Preferred over Full when
	// the passage locator produced per-sentence scores.
	FullSentences []ShadedSentence
	// FocusStart/FocusEnd are the rune offsets of the located window within
	// the full chunk (both 0 when nothing was located). Later steps thread
	// these into the transcript reader and excerpt form.
	FocusStart int
	FocusEnd   int
	Score      float64
	// Match is why this passage matched: "keyword", "semantic", or "hybrid".
	Match string
	// ScoreNorm is Score scaled to 0..1 against the top result of this render.
	ScoreNorm float64
}

// HasMore reports whether an expand-in-place toggle should be offered.
func (c Candidate) HasMore() bool { return len(c.Full) > 0 || len(c.FullSentences) > 0 }

// SnippetSeg is one run of a candidate snippet. Mark=true means it matched a
// query term and should be visually highlighted.
type SnippetSeg struct {
	Text string
	Mark bool
}

// ShadedSentence is one sentence of an expanded candidate, with a normalized
// relevance score (0..1) driving its background shade.
type ShadedSentence struct {
	Segments []SnippetSeg
	Score    float64
}

// EvidencePane is the render model for the #evidence fragment.
type EvidencePane struct {
	Query      string
	NodeID     string
	Candidates []Candidate
	// Mode is the retrieval mode this render was fetched with — "hybrid"
	// (default), "keyword", or "semantic". Drives the toggle's active state
	// on first paint / a full page reload; the client-side $searchMode
	// signal takes over after that.
	Mode string
	// SemanticUnavailable is true when Mode is "semantic" but no embedder is
	// configured, so Candidates is empty for that reason rather than a
	// genuine no-match.
	SemanticUnavailable bool
}

// ReaderSegment is one chunk's text within the transcript reader window.
type ReaderSegment struct {
	ChunkID   string
	Text      string
	Focus     bool
	CharStart int // absolute rune offset of this segment within the source file
	// FocusStart/FocusEnd are rune offsets within Text of the located span
	// carried over from the evidence card. FocusEnd <= FocusStart means none.
	FocusStart int
	FocusEnd   int
}

// FocusSplit is Text cut into the run before, inside, and after the located span.
type FocusSplit struct{ Pre, Mid, Post string }

// HasFocus reports whether a located span should be highlighted in this segment.
func (s ReaderSegment) HasFocus() bool { return s.FocusEnd > s.FocusStart }

// FocusParts splits Text around the located span (rune offsets, clamped).
func (s ReaderSegment) FocusParts() FocusSplit {
	r := []rune(s.Text)
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > len(r) {
			return len(r)
		}
		return v
	}
	a, b := clamp(s.FocusStart), clamp(s.FocusEnd)
	if a > b {
		a = b
	}
	return FocusSplit{Pre: string(r[:a]), Mid: string(r[a:b]), Post: string(r[b:])}
}

// TranscriptReader is the render model for the #transcript-reader fragment.
type TranscriptReader struct {
	NodeID     string // bullet the reader was opened from (attach target)
	SourceFile string
	FocusChunk string
	Segments   []ReaderSegment
	HasPrev    bool
	HasNext    bool
	PrevChunk  string
	NextChunk  string
	// ExcerptStart/ExcerptEnd/ExcerptText prefill the #excerpt-form with the
	// located span (focus-chunk-relative rune offsets) so "Add as evidence"
	// attaches the relevant passage by default. ExcerptText == "" means none.
	ExcerptStart int
	ExcerptEnd   int
	ExcerptText  string
}

// Empty reports whether the reader has nothing to show (closed state).
func (r TranscriptReader) Empty() bool { return r.FocusChunk == "" }

// HasExcerpt reports whether the excerpt form has a located span to prefill.
func (r TranscriptReader) HasExcerpt() bool { return r.ExcerptText != "" }

// OutlineNode is one bullet in the rendered outline tree. Prev/Next are the
// flattened-order neighbour ids for keyboard navigation.
type OutlineNode struct {
	ID       string
	Title    string
	Body     string
	Type     string // "outline" | "chunk_ref"
	Version  int
	Locked   bool
	Evidence bool // chunk_ref evidence sub-bullet
	Depth    int
	Prev     string
	Next     string
	InThread bool   // member of the currently-rendered thread
	Diff     string // "" | "added" | "changed" (diff mode)
	Ghost    bool   // removed-since-baseline bullet, shown for Rescue
	Children []OutlineNode
}

// Outline is the render model for the #outline fragment.
type Outline struct {
	Roots       []OutlineNode
	ThreadID    string // the thread this render is scoped to ("" = none)
	DiffAgainst string // snapshot/branch id being diffed against ("" = none)
}

// ProjectBar is the render model for the #project-bar picker.
type ProjectBar struct {
	Current   string
	Names     []string
	CanSwitch bool
	Switching string // non-empty while a switch is in progress
}

// ThreadRow is one thread in the thread bar.
type ThreadRow struct {
	ID   string
	Name string
}

// ThreadBar is the render model for the #thread-bar fragment.
type ThreadBar struct {
	Threads []ThreadRow
}

// ExitRow is one outgoing CYOA edge from a bullet.
type ExitRow struct {
	EdgeID    string
	ToID      string
	ToTitle   string
	Type      string // "branch" | "jump" | "linear"
	Condition string
}

// ExitOption is a bullet a new exit can point at.
type ExitOption struct {
	ID    string
	Title string
}

// NodeExits is the render model for a bullet's #exits-<id> fragment.
type NodeExits struct {
	NodeID  string
	Exits   []ExitRow
	Options []ExitOption
}

// Empty reports whether the outline has no bullets at all.
func (o Outline) Empty() bool { return len(o.Roots) == 0 }

// SnapshotRow is one saved snapshot in the snapshot bar.
type SnapshotRow struct {
	ID      string
	Label   string
	Created string // pre-formatted for display
}

// SnapshotBar is the render model for the #snapshot-bar fragment.
type SnapshotBar struct {
	Snapshots []SnapshotRow
}

// BranchRow is one branch in the branch bar.
type BranchRow struct {
	ID     string
	Name   string
	Active bool
}

// BranchBar is the render model for the #branch-bar fragment.
type BranchBar struct {
	Branches []BranchRow
}

// StitchSpan is one attributed run of the linearized manuscript preview.
type StitchSpan struct {
	Glue bool // true = AI-generated transition, false = verbatim passage
	Text string
}

// StitchView is the render model for the #stitch fragment (Preview tab).
type StitchView struct {
	ThreadID     string
	Glue         int
	NodeCount    int
	TotalChars   int
	GlueRatioPct int
	Spans        []StitchSpan
	Err          string
}

// UploadRow is one transcript in the ingest drawer list.
type UploadRow struct {
	ID       string
	Filename string
	Status   string // pending | transcribing | done | failed
	Error    string
}

// IngestDrawer is the render model for the #ingest fragment.
type IngestDrawer struct {
	Uploads []UploadRow // newest first
	Total   int         // total uploads (Uploads may be a truncated view)
	Done    int
	Failed  int
	Pending int // pending + processing
	// Active is true while any upload is still pending/processing, so the
	// fragment can poll itself for status updates.
	Active bool
	// Notice is a one-shot status line shown after a server-side path ingest
	// (e.g. "Queued 3 transcript(s)…" or an error). Empty most of the time.
	Notice string
}

// --- Source Atlas (the transcript network; NOT the outline) ---

// AtlasPane is the render model for the #atlas fragment: the list of regions
// in the current build, or a build/empty prompt.
type AtlasPane struct {
	Status   string // "none" | "building" | "ready" | "failed"
	Error    string
	Building bool
	// Ready-state fields:
	Regions    []AtlasRegionRow
	RegionOpen string // id of the region whose detail is expanded, if any
	ChunkCount int
	Stale      bool         // corpus has grown noticeably since this build
	Behind     int          // chunks added since the build (when Stale)
	Matches    AtlasMatches // regions ranked for the focused bullet (may be empty)
}

// AtlasMatches is the #atlas-matches fragment: regions ranked by similarity to
// a query — a search string or the focused bullet's prose.
type AtlasMatches struct {
	Label   string // e.g. "sources for this bullet" or the search query
	Regions []AtlasRegionRow
}

// AtlasRegionRow is one region in the list.
type AtlasRegionRow struct {
	ID         string
	Title      string
	Keywords   []string
	ChunkCount int
}

// AtlasRegionDetail is the render model for #atlas-region: one region's digest,
// its member chunks (as evidence Candidates), and its linked neighbours.
type AtlasRegionDetail struct {
	ID         string
	Title      string
	Abstract   string
	Keywords   []string
	Source     string // "extractive" | "llm:<model>" | ...
	Members    []Candidate
	Neighbours []AtlasRegionRow // linked regions, strongest link first
}
