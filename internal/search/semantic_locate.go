package search

import (
	"context"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// SemanticLocate locates the most query-relevant passage of a chunk by
// embedding the query and each of the chunk's sentences and scoring by cosine
// similarity. It is the fallback for hits that came from the vector arm and
// carry no lexical query-term match to highlight (LocatePassage/Locate return
// nothing). The Window is the best sentence expanded by up to one context
// sentence per side; Sentences carries every sentence's normalized similarity
// for shading. Hits is left empty — there is no lexical span to bold.
//
// Returns a zero result when emb is nil, the chunk has no sentences, embedding
// fails, or nothing clears opts.MinSimilarity.
func SemanticLocate(ctx context.Context, emb ingest.Embedder, chunk, query string, opts LocateOptions) LocateResult {
	if emb == nil {
		return LocateResult{}
	}
	sents := sentenceSpans(chunk)
	if len(sents) == 0 {
		return LocateResult{}
	}

	texts := make([]string, 0, len(sents)+1)
	texts = append(texts, query)
	runes := []rune(chunk)
	for _, s := range sents {
		texts = append(texts, string(runes[s.Start:s.End]))
	}
	vecs, err := emb.EmbedBatch(ctx, texts)
	if err != nil || len(vecs) != len(texts) {
		return LocateResult{}
	}

	qv := vecs[0]
	sims := make([]float64, len(sents))
	best, maxSim := 0, -1.0
	for i := range sents {
		s := ingest.CosineSimilarity(qv, vecs[i+1])
		sims[i] = s
		if s > maxSim {
			maxSim, best = s, i
		}
	}

	minSim := opts.MinSimilarity
	if minSim == 0 {
		minSim = 0.2
	}
	if maxSim < minSim {
		return LocateResult{}
	}

	out := LocateResult{Found: true, Score: maxSim}
	out.Sentences = make([]ScoredSpan, len(sents))
	for i, s := range sents {
		norm := 0.0
		if maxSim > 0 {
			norm = sims[i] / maxSim
		}
		if norm < 0 {
			norm = 0
		}
		out.Sentences[i] = ScoredSpan{Span: s, Score: norm}
	}

	maxW := opts.maxWindow()
	lo, hi := best, best
	winLen := sents[best].End - sents[best].Start
	if hi+1 < len(sents) && winLen+(sents[hi+1].End-sents[hi].End) <= maxW {
		winLen += sents[hi+1].End - sents[hi].End
		hi++
	}
	if lo-1 >= 0 && winLen+(sents[lo].Start-sents[lo-1].Start) <= maxW {
		lo--
	}
	out.Window = Span{sents[lo].Start, sents[hi].End}
	return out
}
