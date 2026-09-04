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

// DefaultPageSize is how many passages the evidence pane shows per page.
const DefaultPageSize = 12

// PageSizeOptions are the page sizes the pager's selector offers. Requests
// for any other size are clamped to the nearest of these (see ClampPageSize).
var PageSizeOptions = []int{12, 25, 50, 100}

// ClampPageSize maps an arbitrary requested page size onto the nearest
// PageSizeOptions entry, so a request can't force an unbounded amount of
// per-candidate snippet work. n <= 0 yields DefaultPageSize.
func ClampPageSize(n int) int {
	if n <= 0 {
		return DefaultPageSize
	}
	best := PageSizeOptions[0]
	for _, opt := range PageSizeOptions {
		if opt == n {
			return opt
		}
		if abs(opt-n) < abs(best-n) {
			best = opt
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// snippetRunes caps a candidate's displayed text.
const snippetRunes = 320

// FindPage runs the hybrid index over query and resolves one page of hits to
// candidates. A blank query returns a zero FindResult (the pane shows its
// idle prompt). mode selects the retrieval arm(s) — see search.Mode; an
// empty/unrecognized mode is hybrid. page is 1-based and gets clamped into
// [1, TotalPages] (or 1 when there are no results) — the effective page
// served is on the returned FindResult, so the caller doesn't need to
// duplicate that logic to know what it actually got.
//
// The full ranked pool is fetched once per call and only the requested page
// is resolved into candidates (snippet/highlight computation happens per
// candidate, so this keeps that work bounded by pageSize, not by however
// many chunks matched overall).
type FindResult struct {
	Candidates []viewmodel.Candidate
	Total      int
	Page       int
	PageSize   int
}

func (f *EvidenceFinder) FindPage(ctx context.Context, query string, mode search.Mode, page, pageSize int) (FindResult, error) {
	pageSize = ClampPageSize(pageSize)
	if page < 1 {
		page = 1
	}
	query = strings.TrimSpace(query)
	if query == "" || f.Index == nil {
		return FindResult{Page: 1, PageSize: pageSize}, nil
	}

	// A draft bullet is prose, not a query — strip stop words and filler so
	// both retrieval and the locator work from the salient terms.
	q := search.CleanQuery(query)

	all, err := f.Index.SearchMode(ctx, q, 0, mode) // full ranked pool
	if err != nil {
		return FindResult{Page: 1, PageSize: pageSize}, err
	}

	total := len(all)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// topScore anchors ScoreNorm to the pool's best match (page 1's top),
	// not the current page's, so the relevance bar stays comparable across
	// pages — a page-3 item scoring "40%" means 40% of the best match
	// overall, not 40% of its weaker page-mates.
	topScore := 0.0
	if total > 0 {
		topScore = all[0].Score
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	results := all[start:end]

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
		if !loc.Found && f.Index.Embedder != nil {
			// Vector-only hit with no lexical term to highlight — score the
			// chunk's sentences against the query embedding instead.
			loc = search.SemanticLocate(ctx, f.Index.Embedder, c.Content, query, opts)
		}
		if !loc.Found && r.Passage.End > r.Passage.Start {
			// Nothing localized, but the passage arm matched a sub-chunk unit —
			// use its span as the focus.
			loc = search.LocateResult{Found: true, Window: r.Passage, Score: r.Score}
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
	return FindResult{Candidates: out, Total: total, Page: page, PageSize: pageSize}, nil
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
