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
