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
