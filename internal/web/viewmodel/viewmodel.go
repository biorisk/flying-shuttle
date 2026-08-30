// Package viewmodel holds the plain data structs passed from web handlers into
// templ components. It is a leaf package (imports nothing from internal/web or
// internal/web/components) so both sides can depend on it without a cycle.
package viewmodel

// Candidate is one ranked supporting passage surfaced for a bullet.
type Candidate struct {
	ChunkID    string
	SourceFile string
	Speaker    string
	Snippet    string
	Score      float64
}

// EvidencePane is the render model for the #evidence fragment.
type EvidencePane struct {
	Query      string
	NodeID     string
	Candidates []Candidate
}

// ReaderSegment is one chunk's text within the transcript reader window.
type ReaderSegment struct {
	ChunkID   string
	Text      string
	Focus     bool
	CharStart int // absolute rune offset of this segment within the source file
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
}

// Empty reports whether the reader has nothing to show (closed state).
func (r TranscriptReader) Empty() bool { return r.FocusChunk == "" }

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
	Children []OutlineNode
}

// Outline is the render model for the #outline fragment.
type Outline struct {
	Roots []OutlineNode
}

// Empty reports whether the outline has no bullets at all.
func (o Outline) Empty() bool { return len(o.Roots) == 0 }

// UploadRow is one transcript in the ingest drawer list.
type UploadRow struct {
	ID       string
	Filename string
	Status   string // pending | transcribing | done | failed
	Error    string
}

// IngestDrawer is the render model for the #ingest fragment.
type IngestDrawer struct {
	Uploads []UploadRow
	// Active is true while any upload is still pending/processing, so the
	// fragment can poll itself for status updates.
	Active bool
}
