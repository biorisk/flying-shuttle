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

	topScore := 0.0
	if len(results) > 0 {
		topScore = results[0].Score
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
			Match:      r.MatchKind(),
		}
		if topScore > 0 {
			cand.ScoreNorm = r.Score / topScore
		}
		// Sentence-granular passage scoring first; fall back to the raw
		// term-window locator for hits that don't land in any sentence.
		opts := search.LocateOptions{MaxWindowRunes: snippetRunes}
		loc := search.LocatePassage(c.Content, q, f.Index.BM25.IDF, opts)
		if !loc.Found {
			loc = search.Locate(c.Content, q, f.Index.BM25.IDF, opts)
		}
		if loc.Found {
			cand.Snippet, cand.Segments = buildSnippet(c.Content, loc.Window, loc.Hits)
			cand.FocusStart, cand.FocusEnd = loc.Window.Start, loc.Window.End
			full := []rune(c.Content)
			clipped := loc.Window.Start > 0 || loc.Window.End < len(full)
			if clipped && len(loc.Sentences) > 0 {
				// Expanded view: whole chunk, one shaded run per sentence.
				for _, sc := range loc.Sentences {
					cand.FullSentences = append(cand.FullSentences, viewmodel.ShadedSentence{
						Segments: segmentize(full, sc.Span, loc.Hits),
						Score:    sc.Score,
					})
				}
				// When hits cluster in 2–3 separated sentences, show them as
				// "… A … B …" rather than one contiguous window.
				if plain, segs, focus, ok := multiSpanSnippet(full, loc.Sentences, loc.Hits, snippetRunes); ok {
					cand.Snippet, cand.Segments = plain, segs
					cand.FocusStart, cand.FocusEnd = focus.Start, focus.End
				}
			} else if clipped {
				_, cand.Full = buildSnippet(c.Content, search.Span{Start: 0, End: len(full)}, loc.Hits)
			}
		} else {
			cand.Snippet = trimRunes(c.Content, snippetRunes)
		}
		out = append(out, cand)
	}
	return out, nil
}

// segmentize splits runes[win] into verbatim and hit-marked (<mark>) segments.
// Offsets are rune-based; hits outside the window are ignored.
func segmentize(runes []rune, win search.Span, hits []search.Span) []viewmodel.SnippetSeg {
	if win.Start < 0 {
		win.Start = 0
	}
	if win.End > len(runes) {
		win.End = len(runes)
	}

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
	return segs
}

// buildSnippet renders the located window of content as display text plus a
// segment list with query-term hits marked. A window that does not reach a
// chunk edge gets an ellipsis on that side.
func buildSnippet(content string, win search.Span, hits []search.Span) (string, []viewmodel.SnippetSeg) {
	runes := []rune(content)
	if win.Start < 0 {
		win.Start = 0
	}
	if win.End > len(runes) {
		win.End = len(runes)
	}
	segs := segmentize(runes, win, hits)
	if win.Start > 0 {
		segs = append([]viewmodel.SnippetSeg{{Text: "…"}}, segs...)
	}
	if win.End < len(runes) {
		segs = append(segs, viewmodel.SnippetSeg{Text: "…"})
	}

	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String(), segs
}

// multiSpanSnippet builds a "… A … B …" snippet from the 2–3 highest-scoring,
// non-adjacent sentences of a chunk. Returns ok=false when the hits don't
// actually spread out (one dominant region), so the caller keeps its single
// contiguous window. focus is the span of the top-scoring group, for the
// transcript reader.
func multiSpanSnippet(runes []rune, sents []search.ScoredSpan, hits []search.Span, budget int) (plain string, segs []viewmodel.SnippetSeg, focus search.Span, ok bool) {
	const minScore = 0.5

	// Selected sentence indices, in document order.
	var sel []int
	for i, s := range sents {
		if s.Score >= minScore {
			sel = append(sel, i)
		}
	}
	if len(sel) < 2 {
		return "", nil, search.Span{}, false
	}

	// Group runs of consecutive selected sentences.
	type group struct {
		span   search.Span
		score  float64
		endIdx int
	}
	var groups []group
	for _, i := range sel {
		if n := len(groups); n > 0 && i == groups[n-1].endIdx+1 {
			groups[n-1].span.End = sents[i].End
			if sents[i].Score > groups[n-1].score {
				groups[n-1].score = sents[i].Score
			}
			groups[n-1].endIdx = i
			continue
		}
		groups = append(groups, group{span: sents[i].Span, score: sents[i].Score, endIdx: i})
	}
	if len(groups) < 2 {
		return "", nil, search.Span{}, false // one contiguous region
	}

	// Keep at most 3 groups, highest score first, then restore document order.
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].score > groups[b].score })
	if len(groups) > 3 {
		groups = groups[:3]
	}
	// Drop lowest-scoring groups until the total width fits the budget (keep ≥2).
	width := func(gs []group) int {
		w := 0
		for _, g := range gs {
			w += g.span.End - g.span.Start
		}
		return w + 3*len(gs) // rough separator/ellipsis allowance
	}
	for len(groups) > 2 && width(groups) > budget {
		groups = groups[:len(groups)-1]
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].span.Start < groups[b].span.Start })
	if width(groups) > budget {
		return "", nil, search.Span{}, false
	}

	segs = append(segs, viewmodel.SnippetSeg{Text: "…"})
	for gi, g := range groups {
		if gi > 0 {
			segs = append(segs, viewmodel.SnippetSeg{Text: " … "})
		}
		segs = append(segs, segmentize(runes, g.span, hits)...)
	}
	segs = append(segs, viewmodel.SnippetSeg{Text: "…"})

	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	// focus = the highest-scoring group's span.
	focus = groups[0].span
	best := groups[0].score
	for _, g := range groups {
		if g.score > best {
			best, focus = g.score, g.span
		}
	}
	return b.String(), segs, focus, true
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
