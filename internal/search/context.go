package search

import "context"

// ContextCheck is the result of verifying a node's chunks against a parent context.
type ContextCheck struct {
	InContext bool    `json:"in_context"`
	Score     float64 `json:"score"`             // 0–1 relevance of node chunks to parent
	Message   string  `json:"message,omitempty"` // warning text when out of context
}

// ContextChecker verifies whether a node's evidence fits a parent's semantic context.
type ContextChecker struct {
	Index *HybridIndex
	// Threshold is the minimum score (0–1) for a node to be considered in context.
	// Default 0.3.
	Threshold float64
}

// Check searches the index using parentTitle as query and evaluates how well
// the given chunkIDs score in the results. Returns an in-context assessment.
func (cc *ContextChecker) Check(ctx context.Context, parentTitle string, chunkIDs []string) (*ContextCheck, error) {
	if len(chunkIDs) == 0 || parentTitle == "" {
		return &ContextCheck{InContext: true, Score: 1.0}, nil
	}

	threshold := cc.Threshold
	if threshold <= 0 {
		threshold = 0.3
	}

	// Search with parent context — get a generous set of candidates.
	results, err := cc.Index.Search(ctx, parentTitle, 50)
	if err != nil {
		return nil, err
	}

	// Build a score map from search results.
	scoreMap := make(map[string]float64, len(results))
	maxScore := 0.0
	for _, r := range results {
		scoreMap[r.ChunkID] = r.Score
		if r.Score > maxScore {
			maxScore = r.Score
		}
	}

	// Compute average normalized score for the node's chunks.
	totalScore := 0.0
	matched := 0
	for _, cid := range chunkIDs {
		if s, ok := scoreMap[cid]; ok {
			if maxScore > 0 {
				totalScore += s / maxScore
			}
			matched++
		}
	}

	var avgScore float64
	if len(chunkIDs) > 0 {
		avgScore = totalScore / float64(len(chunkIDs))
	}

	check := &ContextCheck{
		InContext: avgScore >= threshold,
		Score:     avgScore,
	}
	if !check.InContext {
		check.Message = "Out of Context"
	}
	return check, nil
}
