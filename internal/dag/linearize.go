package dag

import (
	"context"
	"fmt"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/doc"
)

// LinearizeMode controls which nodes are included and their order.
type LinearizeMode string

const (
	// ModeThread linearizes a single thread's ordered nodes.
	ModeThread LinearizeMode = "thread"
	// ModeManuscript linearizes all nodes in topological order (full manuscript).
	ModeManuscript LinearizeMode = "manuscript"
)

// LinearizeRequest configures a linearization operation.
type LinearizeRequest struct {
	Mode      LinearizeMode
	ThreadID  string // required when Mode == ModeThread
	GlueLevel int    // 0-100, passed to stitcher
}

// LinearizeResult is the output of a linearize-and-stitch operation.
type LinearizeResult struct {
	Nodes  []model.Node   `json:"nodes"`
	Stitch *stitch.Result `json:"stitch"`
}

// LinearizeAndStitch collects nodes in the requested order, gathers their
// chunks, and passes them through the stitcher to produce a continuous document.
// Shared chunks that appear in multiple nodes are included only once (first
// occurrence), preserving attribution while avoiding duplication.
func LinearizeAndStitch(ctx context.Context, s doc.Store, stitcher stitch.Stitcher, req LinearizeRequest) (*LinearizeResult, error) {
	var nodes []model.Node
	var err error

	switch req.Mode {
	case ModeManuscript:
		nodes, err = TopologicalSort(s)
	default:
		if req.ThreadID == "" {
			return nil, fmt.Errorf("thread_id required for thread mode")
		}
		nodes, err = Linearize(s, req.ThreadID)
	}
	if err != nil {
		return nil, err
	}

	chunks := collectChunks(s, nodes)

	result, err := stitcher.Stitch(ctx, stitch.Request{
		Chunks:    chunks,
		GlueLevel: req.GlueLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("stitch: %w", err)
	}

	return &LinearizeResult{
		Nodes:  nodes,
		Stitch: result,
	}, nil
}

// collectChunks gathers stitch inputs from an ordered list of nodes.
//
// For each node it emits one input per attached evidence row, using the
// evidence's *excerpt text* verbatim — never the whole source chunk — so the
// stitched manuscript only ever contains text the writer explicitly chose.
// A node with no evidence falls back to its body text. Identical spans
// (same chunk + same offsets) appearing under multiple nodes are emitted once.
func collectChunks(s doc.Store, nodes []model.Node) []stitch.ChunkInput {
	var chunks []stitch.ChunkInput
	seenSpan := make(map[string]bool)
	seenBody := make(map[string]bool)

	for _, n := range nodes {
		evidence, err := s.ListNodeEvidence(n.ID)
		if err != nil || len(evidence) == 0 {
			if n.Body != "" && !seenBody[n.ID] {
				seenBody[n.ID] = true
				chunks = append(chunks, stitch.ChunkInput{ID: n.ID, Content: n.Body})
			}
			continue
		}
		for _, e := range evidence {
			key := fmt.Sprintf("%s:%d:%d", e.ChunkID, e.CharStart, e.CharEnd)
			if seenSpan[key] {
				continue
			}
			seenSpan[key] = true

			speaker := ""
			if c, err := s.GetChunk(e.ChunkID); err == nil && c.Speaker != nil {
				speaker = *c.Speaker
			}
			chunks = append(chunks, stitch.ChunkInput{
				ID:      e.ChunkID,
				Content: e.Text,
				Speaker: speaker,
			})
		}
	}
	return chunks
}
