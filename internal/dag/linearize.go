package dag

import (
	"context"
	"fmt"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
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
func LinearizeAndStitch(ctx context.Context, s store.Store, stitcher stitch.Stitcher, req LinearizeRequest) (*LinearizeResult, error) {
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
// For each node, it uses associated chunks if available; otherwise it falls
// back to the node's body text. Shared chunks are deduplicated by ID.
func collectChunks(s store.Store, nodes []model.Node) []stitch.ChunkInput {
	var chunks []stitch.ChunkInput
	seen := make(map[string]bool)

	for _, n := range nodes {
		nodeChunks, err := s.GetNodeChunks(n.ID)
		if err != nil || len(nodeChunks) == 0 {
			// No chunks — use node body as content.
			if n.Body != "" && !seen[n.ID] {
				seen[n.ID] = true
				chunks = append(chunks, stitch.ChunkInput{
					ID:      n.ID,
					Content: n.Body,
				})
			}
			continue
		}
		for _, c := range nodeChunks {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			speaker := ""
			if c.Speaker != nil {
				speaker = *c.Speaker
			}
			chunks = append(chunks, stitch.ChunkInput{
				ID:      c.ID,
				Content: c.Content,
				Speaker: speaker,
			})
		}
	}
	return chunks
}
