package web

import (
	"context"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// EvidenceFinder turns bullet text into ranked candidate passages. It is the
// single retrieval path for the outline editor — there is no separate search.
type EvidenceFinder struct {
	Index *search.HybridIndex
	Store store.Store
}

// DefaultCandidateLimit is how many passages the evidence pane shows.
const DefaultCandidateLimit = 12

// snippetRunes caps a candidate's displayed text.
const snippetRunes = 320

// Find runs the hybrid index over query and resolves the hits to candidates.
// A blank query returns nil (the pane shows its idle prompt).
func (f *EvidenceFinder) Find(ctx context.Context, query string, limit int) ([]viewmodel.Candidate, error) {
	query = strings.TrimSpace(query)
	if query == "" || f.Index == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = DefaultCandidateLimit
	}

	results, err := f.Index.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	out := make([]viewmodel.Candidate, 0, len(results))
	for _, r := range results {
		c, err := f.Store.GetChunk(r.ChunkID)
		if err != nil {
			continue // chunk vanished between index and store; skip
		}
		speaker := ""
		if c.Speaker != nil {
			speaker = *c.Speaker
		}
		out = append(out, viewmodel.Candidate{
			ChunkID:    c.ID,
			SourceFile: c.SourceFile,
			Speaker:    speaker,
			Snippet:    trimRunes(c.Content, snippetRunes),
			Score:      r.Score,
		})
	}
	return out, nil
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
