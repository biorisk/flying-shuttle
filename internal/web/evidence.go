package web

import (
	"context"
	"sort"
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

	// A draft bullet is prose, not a query — strip stop words and filler so
	// both retrieval and the locator work from the salient terms.
	q := search.CleanQuery(query)

	results, err := f.Index.Search(ctx, q, limit)
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
		cand := viewmodel.Candidate{
			ChunkID:    c.ID,
			SourceFile: c.SourceFile,
			Speaker:    speaker,
			Score:      r.Score,
		}
		loc := search.Locate(c.Content, q, f.Index.BM25.IDF, search.LocateOptions{MaxWindowRunes: snippetRunes})
		if loc.Found {
			cand.Snippet, cand.Segments = buildSnippet(c.Content, loc.Window, loc.Hits)
			cand.FocusStart, cand.FocusEnd = loc.Window.Start, loc.Window.End
			if loc.Window.Start > 0 || loc.Window.End < len([]rune(c.Content)) {
				full := []rune(c.Content)
				_, cand.Full = buildSnippet(c.Content, search.Span{Start: 0, End: len(full)}, loc.Hits)
			}
		} else {
			cand.Snippet = trimRunes(c.Content, snippetRunes)
		}
		out = append(out, cand)
	}
	return out, nil
}

// buildSnippet renders the located window of content as display text plus a
// segment list with query-term hits marked. Offsets are rune-based. A window
// that does not reach a chunk edge gets an ellipsis on that side.
func buildSnippet(content string, win search.Span, hits []search.Span) (string, []viewmodel.SnippetSeg) {
	runes := []rune(content)
	if win.Start < 0 {
		win.Start = 0
	}
	if win.End > len(runes) {
		win.End = len(runes)
	}

	// Clip hits to the window and sort by start.
	type span struct{ s, e int }
	marks := make([]span, 0, len(hits))
	for _, h := range hits {
		s, e := max(h.Start, win.Start), min(h.End, win.End)
		if s < e {
			marks = append(marks, span{s, e})
		}
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].s < marks[j].s })

	var segs []viewmodel.SnippetSeg
	add := func(s, e int, mark bool) {
		if s < e {
			segs = append(segs, viewmodel.SnippetSeg{Text: string(runes[s:e]), Mark: mark})
		}
	}
	if win.Start > 0 {
		segs = append(segs, viewmodel.SnippetSeg{Text: "…"})
	}
	pos := win.Start
	for _, mk := range marks {
		if mk.s < pos { // overlaps the previous mark — extend, don't nest
			if mk.e > pos {
				add(pos, mk.e, true)
				pos = mk.e
			}
			continue
		}
		add(pos, mk.s, false)
		add(mk.s, mk.e, true)
		pos = mk.e
	}
	add(pos, win.End, false)
	if win.End < len(runes) {
		segs = append(segs, viewmodel.SnippetSeg{Text: "…"})
	}

	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String(), segs
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
