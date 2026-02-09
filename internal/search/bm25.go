package search

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25Index is an in-memory inverted index implementing the Okapi BM25
// ranking function for keyword-based retrieval of chunks.
type BM25Index struct {
	k1  float64 // term frequency saturation, typically 1.2
	b   float64 // length normalization, typically 0.75
	avg float64 // average document length

	// docLen[chunkID] = number of tokens in that chunk
	docLen map[string]int

	// postings[term] = map[chunkID]termFrequency
	postings map[string]map[string]int

	// total number of indexed documents
	numDocs int
}

// NewBM25Index creates an empty BM25 index with default parameters.
func NewBM25Index() *BM25Index {
	return &BM25Index{
		k1:       1.2,
		b:        0.75,
		docLen:   make(map[string]int),
		postings: make(map[string]map[string]int),
	}
}

// Add indexes a document (chunk) by its ID and text content.
func (idx *BM25Index) Add(id, text string) {
	tokens := tokenize(text)
	idx.docLen[id] = len(tokens)
	idx.numDocs++

	// Update running average document length.
	total := 0
	for _, l := range idx.docLen {
		total += l
	}
	idx.avg = float64(total) / float64(idx.numDocs)

	// Update postings.
	freq := make(map[string]int)
	for _, t := range tokens {
		freq[t]++
	}
	for term, count := range freq {
		if idx.postings[term] == nil {
			idx.postings[term] = make(map[string]int)
		}
		idx.postings[term][id] = count
	}
}

// Search returns ranked results for the given query, up to limit.
func (idx *BM25Index) Search(query string, limit int) []Result {
	queryTerms := tokenize(query)
	if len(queryTerms) == 0 || idx.numDocs == 0 {
		return nil
	}

	scores := make(map[string]float64)
	for _, term := range queryTerms {
		docs, ok := idx.postings[term]
		if !ok {
			continue
		}
		// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
		df := float64(len(docs))
		n := float64(idx.numDocs)
		idf := math.Log((n-df+0.5)/(df+0.5) + 1.0)

		for docID, tf := range docs {
			dl := float64(idx.docLen[docID])
			// BM25 term score
			num := float64(tf) * (idx.k1 + 1)
			denom := float64(tf) + idx.k1*(1-idx.b+idx.b*dl/idx.avg)
			scores[docID] += idf * num / denom
		}
	}

	results := make([]Result, 0, len(scores))
	for id, score := range scores {
		results = append(results, Result{ChunkID: id, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

// tokenize splits text into lowercase alphanumeric tokens.
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return words
}
