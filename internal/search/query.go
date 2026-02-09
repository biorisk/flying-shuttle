package search

import (
	"context"
	"strings"
)

// Suggestion represents a ranked chunk candidate with a descriptive label.
type Suggestion struct {
	ChunkID    string  `json:"chunk_id"`
	Label      string  `json:"label"`
	Score      float64 `json:"score"`
	Confidence float64 `json:"confidence"`
}

// QueryTranslator converts node text into search queries, runs them against
// the hybrid index, and returns chunk suggestions.
type QueryTranslator struct {
	Index *HybridIndex
}

// TranslateOpts configures a Translate call.
type TranslateOpts struct {
	Title       string
	Body        string
	ParentTitle string
	Limit       int
	// ExcludeIDs is a set of chunk IDs to omit from results (already used).
	ExcludeIDs map[string]bool
}

// Translate takes a node's title and optional body/context, generates multiple
// query variants, searches the hybrid index with each, and merges results.
// Deprecated: use TranslateWithOpts for exclusion support.
func (qt *QueryTranslator) Translate(ctx context.Context, title, body, parentTitle string, limit int) ([]Suggestion, error) {
	return qt.TranslateWithOpts(ctx, TranslateOpts{
		Title:       title,
		Body:        body,
		ParentTitle: parentTitle,
		Limit:       limit,
	})
}

// TranslateWithOpts is the full-featured translation with exclusion support.
func (qt *QueryTranslator) TranslateWithOpts(ctx context.Context, opts TranslateOpts) ([]Suggestion, error) {
	title := opts.Title
	body := opts.Body
	parentTitle := opts.ParentTitle
	limit := opts.Limit
	if limit <= 0 {
		limit = 5
	}

	// Generate query variants for broader recall.
	queries := qt.expandQueries(title, body, parentTitle)
	if len(queries) == 0 {
		return nil, nil
	}

	// Run each query and collect results with RRF.
	var allResults [][]Result
	for _, q := range queries {
		results, err := qt.Index.Search(ctx, q, limit*2)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results)
	}

	// Fuse across all query variants.
	fused := reciprocalRankFusionK(60, allResults...)

	// Filter out already-used chunks.
	if len(opts.ExcludeIDs) > 0 {
		filtered := fused[:0]
		for _, r := range fused {
			if !opts.ExcludeIDs[r.ChunkID] {
				filtered = append(filtered, r)
			}
		}
		fused = filtered
	}

	if len(fused) > limit {
		fused = fused[:limit]
	}

	// Convert to suggestions with labels and confidence scores.
	suggestions := make([]Suggestion, len(fused))
	maxScore := 0.0
	if len(fused) > 0 {
		maxScore = fused[0].Score
	}
	for i, r := range fused {
		confidence := 0.0
		if maxScore > 0 {
			confidence = r.Score / maxScore
		}
		suggestions[i] = Suggestion{
			ChunkID:    r.ChunkID,
			Label:      labelForQuery(title, i),
			Score:      r.Score,
			Confidence: confidence,
		}
	}

	return suggestions, nil
}

// expandQueries generates multiple query variants from node context.
func (qt *QueryTranslator) expandQueries(title, body, parentTitle string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}

	var queries []string

	// 1. Direct title query — captures exact keyword intent.
	queries = append(queries, title)

	// 2. Keyword-extracted query — remove common stop words for focused matching.
	keywords := extractKeywords(title)
	if keywords != title && keywords != "" {
		queries = append(queries, keywords)
	}

	// 3. Title + body snippet — adds semantic depth.
	if body = strings.TrimSpace(body); body != "" {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		queries = append(queries, title+" "+snippet)
	}

	// 4. Context-expanded — parent title provides hierarchical grounding.
	if parentTitle = strings.TrimSpace(parentTitle); parentTitle != "" {
		queries = append(queries, parentTitle+" "+title)
	}

	return queries
}

// labelForQuery generates a descriptive label for a suggestion.
func labelForQuery(title string, rank int) string {
	suffixes := []string{"evidence", "context", "supporting detail", "related passage", "background"}
	idx := rank % len(suffixes)
	return title + " — " + suffixes[idx]
}

// Common English stop words to strip for keyword extraction.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "s": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "shall": true, "can": true,
	"of": true, "in": true, "to": true, "for": true, "with": true,
	"on": true, "at": true, "by": true, "from": true, "about": true,
	"as": true, "into": true, "through": true, "during": true, "before": true,
	"after": true, "above": true, "below": true, "between": true,
	"and": true, "but": true, "or": true, "nor": true, "not": true,
	"so": true, "yet": true, "both": true, "either": true, "neither": true,
	"this": true, "that": true, "these": true, "those": true,
	"it": true, "its": true, "he": true, "she": true, "they": true,
	"we": true, "you": true, "i": true, "me": true, "my": true,
	"his": true, "her": true, "their": true, "our": true, "your": true,
}

// extractKeywords removes stop words and returns remaining terms.
func extractKeywords(text string) string {
	tokens := tokenize(text)
	var keywords []string
	for _, t := range tokens {
		if !stopWords[t] {
			keywords = append(keywords, t)
		}
	}
	return strings.Join(keywords, " ")
}
