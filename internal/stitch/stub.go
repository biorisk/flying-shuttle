package stitch

import "context"

// StubStitcher concatenates chunks with simple ellipsis glue. Useful for
// development and testing without an LLM backend.
type StubStitcher struct{}

func (s *StubStitcher) Stitch(_ context.Context, req Request) (*Result, error) {
	if len(req.Chunks) == 0 {
		return buildResult(nil), nil
	}

	spans := make([]Span, 0, len(req.Chunks)*2)
	for i, c := range req.Chunks {
		if i > 0 {
			spans = append(spans, Span{Type: SpanGlue, Text: " ... "})
		}
		spans = append(spans, Span{
			Type:       SpanChunk,
			ChunkIndex: i,
			ChunkID:    c.ID,
			Text:       c.Content,
		})
	}

	return buildResult(spans), nil
}
