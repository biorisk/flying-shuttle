// Package stitch implements constrained text stitching: ordering raw chunks
// and adding minimal transition words for coherence while preserving the
// original text verbatim. Every span in the output is attributed as either
// original chunk content or AI-generated "glue".
package stitch

import "context"

// SpanType identifies whether a span of text is original chunk content or
// AI-generated transition glue.
type SpanType string

const (
	SpanChunk SpanType = "chunk"
	SpanGlue  SpanType = "glue"
)

// Span is a contiguous segment of stitched output with attribution.
type Span struct {
	Type       SpanType `json:"type"`
	ChunkIndex int      `json:"chunk_index,omitempty"` // only for SpanChunk
	ChunkID    string   `json:"chunk_id,omitempty"`    // only for SpanChunk
	Text       string   `json:"text"`
}

// Request holds the inputs for a stitch operation.
type Request struct {
	// Chunks are the raw text segments to stitch, in the desired order.
	Chunks []ChunkInput `json:"chunks"`

	// GlueLevel controls how much transition text the stitcher may add.
	// 0 = no glue (raw concatenation), 100 = full AI smoothing.
	// Default (0 from caller) is treated as 50.
	GlueLevel int `json:"glue_level"`
}

// ChunkInput is one raw chunk to be stitched.
type ChunkInput struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Speaker string `json:"speaker,omitempty"`
}

// Result is the output of a stitch operation.
type Result struct {
	// Spans is the ordered list of attributed text segments.
	Spans []Span `json:"spans"`

	// Text is the fully concatenated stitched output (convenience field).
	Text string `json:"text"`

	// Stats summarizes the attribution breakdown.
	Stats Stats `json:"stats"`
}

// Stats provides a quick summary of how much of the output is original vs glue.
type Stats struct {
	ChunkChars int     `json:"chunk_chars"`
	GlueChars  int     `json:"glue_chars"`
	TotalChars int     `json:"total_chars"`
	GlueRatio  float64 `json:"glue_ratio"` // glue_chars / total_chars
}

// Stitcher stitches ordered chunks into coherent text with attribution.
type Stitcher interface {
	Stitch(ctx context.Context, req Request) (*Result, error)
}

// buildResult computes the Text and Stats fields from a slice of Spans.
func buildResult(spans []Span) *Result {
	r := &Result{Spans: spans}
	for _, s := range spans {
		r.Text += s.Text
		n := len([]rune(s.Text))
		switch s.Type {
		case SpanChunk:
			r.Stats.ChunkChars += n
		case SpanGlue:
			r.Stats.GlueChars += n
		}
	}
	r.Stats.TotalChars = r.Stats.ChunkChars + r.Stats.GlueChars
	if r.Stats.TotalChars > 0 {
		r.Stats.GlueRatio = float64(r.Stats.GlueChars) / float64(r.Stats.TotalChars)
	}
	return r
}
