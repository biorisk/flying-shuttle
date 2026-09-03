package search

import (
	"math"
	"strings"
	"unicode"
)

// Span is a half-open rune-offset range [Start, End) into a source string.
// Rune offsets match the units used by model.Evidence.CharStart/CharEnd and
// the transcript reader, so a located span can be threaded straight through.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Token is a normalized (lowercased) term together with its rune span in the
// text it was extracted from.
type Token struct {
	Term  string
	Start int
	End   int
}

// tokenizeWithPositions is tokenize with rune-offset spans for each token. The
// term is lowercased to match tokenize; Start/End index the original runes so
// callers can slice the source text for display.
func tokenizeWithPositions(text string) []Token {
	var toks []Token
	start := -1
	runeIdx := 0
	var b strings.Builder
	flush := func(end int) {
		if start >= 0 {
			toks = append(toks, Token{Term: b.String(), Start: start, End: end})
			b.Reset()
			start = -1
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = runeIdx
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			flush(runeIdx)
		}
		runeIdx++
	}
	flush(runeIdx)
	return toks
}

// IDF returns the BM25 inverse-document-frequency weight of term, using the
// same formula as Search. Rare terms score higher. A term absent from the
// index is treated as df=0. Safe for concurrent use.
func (idx *BM25Index) IDF(term string) float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	n := float64(idx.numDocs)
	df := float64(len(idx.postings[strings.ToLower(term)]))
	return math.Log((n-df+0.5)/(df+0.5) + 1.0)
}

// ScoredSpan is a text span with a relevance score normalized to [0,1].
type ScoredSpan struct {
	Span
	Score float64
}

// LocateResult reports the most query-relevant span within a chunk.
type LocateResult struct {
	// Window is the rune span of the best-scoring passage, snapped to token
	// boundaries and at most LocateOptions.MaxWindowRunes wide. When Found is
	// false it is the zero Span.
	Window Span
	// Hits are the rune spans of every query-term occurrence in the whole
	// chunk, in document order — for highlighting individual matches.
	Hits []Span
	// Sentences carries every sentence span of the chunk with its normalized
	// query relevance (max sentence = 1.0). Filled by LocatePassage; nil from
	// Locate.
	Sentences []ScoredSpan
	// Score is the summed IDF of the hits that fall inside Window.
	Score float64
	// Found is true when at least one query term occurs in the chunk.
	Found bool
}

// queryWeights tokenizes query and maps each distinct term to its IDF weight
// (>= 0). A nil idf weights every term at 1.
func queryWeights(query string, idf func(string) float64) map[string]float64 {
	weights := make(map[string]float64)
	for _, t := range tokenizeWithPositions(query) {
		if _, seen := weights[t.Term]; seen {
			continue
		}
		w := 1.0
		if idf != nil {
			w = idf(t.Term)
		}
		if w < 0 {
			w = 0
		}
		weights[t.Term] = w
	}
	return weights
}

// sentenceSpans splits text into rune-offset spans on sentence-ending
// punctuation followed by whitespace — the same rule as ingest.ParseTranscript.
// Leading whitespace is trimmed from each span.
func sentenceSpans(text string) []Span {
	runes := []rune(text)
	var spans []Span
	start := 0
	skipLeadingWS := func(i int) int {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		return i
	}
	start = skipLeadingWS(0)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) {
			if n := runes[i+1]; n == ' ' || n == '\n' || n == '\t' {
				end := i + 1
				if start < end && strings.TrimSpace(string(runes[start:end])) != "" {
					spans = append(spans, Span{start, end})
				}
				start = skipLeadingWS(end)
			}
		}
	}
	if start < len(runes) && strings.TrimSpace(string(runes[start:])) != "" {
		spans = append(spans, Span{start, len(runes)})
	}
	return spans
}

// LocatePassage is the sentence-granular locator: it splits the chunk into
// sentences, scores each by the summed IDF weight of the query-term hits it
// contains, and returns the best sentence expanded by up to one sentence of
// context on each side (bounded by opts.MaxWindowRunes). It also returns every
// sentence's normalized score in Sentences. Falls back to a zero result when no
// query term appears — the caller can then try Locate.
func LocatePassage(chunk, query string, idf func(string) float64, opts LocateOptions) LocateResult {
	weights := queryWeights(query, idf)
	if len(weights) == 0 {
		return LocateResult{}
	}
	sents := sentenceSpans(chunk)
	if len(sents) == 0 {
		return LocateResult{}
	}
	toks := tokenizeWithPositions(chunk)

	raw := make([]float64, len(sents))
	var hits []Span
	maxRaw := 0.0
	for _, t := range toks {
		w, ok := weights[t.Term]
		if !ok {
			continue
		}
		hits = append(hits, Span{t.Start, t.End})
		for k := range sents {
			if t.Start >= sents[k].Start && t.Start < sents[k].End {
				raw[k] += w
				if raw[k] > maxRaw {
					maxRaw = raw[k]
				}
				break
			}
		}
	}
	if len(hits) == 0 || maxRaw == 0 {
		return LocateResult{}
	}

	best := 0
	for k := range raw {
		if raw[k] > raw[best] {
			best = k
		}
	}

	maxW := opts.maxWindow()
	// A single relevant sentence wider than the window (run-on text, missing
	// punctuation) can't be localized here — defer to the term-window locator.
	if sents[best].End-sents[best].Start > maxW {
		return LocateResult{}
	}
	lo, hi := best, best
	winLen := sents[best].End - sents[best].Start
	if hi+1 < len(sents) && winLen+(sents[hi+1].End-sents[hi].End) <= maxW {
		winLen += sents[hi+1].End - sents[hi].End
		hi++
	}
	if lo-1 >= 0 && winLen+(sents[lo].Start-sents[lo-1].Start) <= maxW {
		lo--
	}

	out := LocateResult{Found: true, Score: raw[best]}
	out.Window = Span{sents[lo].Start, sents[hi].End}
	out.Hits = hits
	out.Sentences = make([]ScoredSpan, len(sents))
	for k, s := range sents {
		out.Sentences[k] = ScoredSpan{Span: s, Score: raw[k] / maxRaw}
	}
	return out
}

// LocateOptions tunes Locate.
type LocateOptions struct {
	// MaxWindowRunes caps the width of the located window. Defaults to 240,
	// roughly a sentence or two of transcript prose.
	MaxWindowRunes int
	// MinSimilarity is the cosine floor for SemanticLocate to report a hit.
	// Defaults to 0.2.
	MinSimilarity float64
}

func (o LocateOptions) maxWindow() int {
	if o.MaxWindowRunes > 0 {
		return o.MaxWindowRunes
	}
	return 240
}

// Locate finds the span of text within chunk that best matches query, scoring
// candidate windows by the summed IDF weight of the query-term hits they
// contain — the approach used by Elasticsearch's unified highlighter.
//
// idf supplies the weight for a term; pass BM25Index.IDF (a nil idf weights
// every term equally). query is tokenized here. The returned Window is snapped
// to token boundaries; Hits covers the whole chunk regardless of Window so the
// UI can bold every match it chooses to show.
func Locate(chunk, query string, idf func(string) float64, opts LocateOptions) LocateResult {
	weights := queryWeights(query, idf)
	if len(weights) == 0 {
		return LocateResult{}
	}

	toks := tokenizeWithPositions(chunk)
	type hit struct {
		span   Span
		weight float64
	}
	var hits []hit
	for _, t := range toks {
		if w, ok := weights[t.Term]; ok {
			hits = append(hits, hit{Span{t.Start, t.End}, w})
		}
	}
	if len(hits) == 0 {
		return LocateResult{}
	}

	maxW := opts.maxWindow()
	// For each hit taken as the window's left edge, extend right over every
	// hit that still fits within maxW and sum the weights. The optimal
	// bounded-width window can always be shifted so its left edge coincides
	// with a hit, so this finds the global best in O(hits^2) — and hits is
	// tiny (query-term occurrences in one ~160-word chunk).
	best := -1.0
	var bestI, bestJ int
	for i := range hits {
		sum := 0.0
		j := i
		for j < len(hits) && hits[j].span.End-hits[i].span.Start <= maxW {
			sum += hits[j].weight
			j++
		}
		if sum > best {
			best, bestI, bestJ = sum, i, j-1
		}
	}

	out := LocateResult{Found: true, Score: best}
	out.Hits = make([]Span, len(hits))
	for k, h := range hits {
		out.Hits[k] = h.span
	}

	total := 0
	if len(toks) > 0 {
		total = toks[len(toks)-1].End
	}
	winStart := hits[bestI].span.Start
	winEnd := hits[bestJ].span.End
	// Pad evenly toward maxW so the match sits near the middle of the window
	// rather than flush against its edge.
	if slack := maxW - (winEnd - winStart); slack > 0 {
		winStart -= slack / 2
		winEnd += slack - slack/2
	}
	if winStart < 0 {
		winStart = 0
	}
	if winEnd > total {
		winEnd = total
	}
	winStart, winEnd = snapToTokens(toks, winStart, winEnd)
	out.Window = Span{winStart, winEnd}
	return out
}

// snapToTokens pulls either edge that splits a token inward to the nearest
// token boundary, so the window never cuts a word mid-way and never grows
// past its requested width. If that would collapse the span it is left as-is.
func snapToTokens(toks []Token, start, end int) (int, int) {
	s, e := start, end
	for _, t := range toks {
		if t.Start < start && t.End > start {
			s = t.End // drop the partial leading word
		}
		if t.Start < end && t.End > end {
			e = t.Start // drop the partial trailing word
		}
	}
	if s >= e {
		return start, end
	}
	return s, e
}
