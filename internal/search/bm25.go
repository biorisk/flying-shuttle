package search

import (
	"encoding/gob"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// BM25Index is an in-memory inverted index implementing the Okapi BM25
// ranking function for keyword-based retrieval of chunks.
//
// Add is idempotent: re-adding an existing ID replaces its previous state,
// so the index can be safely reconciled against the source of truth.
type BM25Index struct {
	mu sync.RWMutex

	k1 float64 // term frequency saturation, typically 1.2
	b  float64 // length normalization, typically 0.75

	total int     // sum of all document lengths (for O(1) average)
	avg   float64 // average document length

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

// Add indexes a document (chunk) by its ID and text content. If the ID is
// already present its previous tokens are removed first.
func (idx *BM25Index) Add(id, text string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, exists := idx.docLen[id]; exists {
		idx.remove(id)
	}

	tokens := tokenize(text)
	idx.docLen[id] = len(tokens)
	idx.total += len(tokens)
	idx.numDocs++
	idx.avg = float64(idx.total) / float64(idx.numDocs)

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

// remove deletes a document's postings and length bookkeeping.
// The caller must hold idx.mu for writing.
func (idx *BM25Index) remove(id string) {
	l, ok := idx.docLen[id]
	if !ok {
		return
	}
	delete(idx.docLen, id)
	idx.total -= l
	idx.numDocs--
	if idx.numDocs > 0 {
		idx.avg = float64(idx.total) / float64(idx.numDocs)
	} else {
		idx.avg = 0
	}
	for term, docs := range idx.postings {
		if _, ok := docs[id]; ok {
			delete(docs, id)
			if len(docs) == 0 {
				delete(idx.postings, term)
			}
		}
	}
}

// Has reports whether a chunk ID is currently indexed.
func (idx *BM25Index) Has(id string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.docLen[id]
	return ok
}

// Len returns the number of indexed documents.
func (idx *BM25Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.numDocs
}

// Search returns ranked results for the given query, up to limit.
func (idx *BM25Index) Search(query string, limit int) []Result {
	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if idx.numDocs == 0 {
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

// bm25Snapshot is the on-disk representation of a BM25Index. gob cannot see
// unexported fields, so the index is projected onto this struct for encoding.
type bm25Snapshot struct {
	K1, B    float64
	Total    int
	Avg      float64
	DocLen   map[string]int
	Postings map[string]map[string]int
	NumDocs  int
}

// Save gob-encodes the whole index to w.
func (idx *BM25Index) Save(w io.Writer) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return gob.NewEncoder(w).Encode(bm25Snapshot{
		K1:       idx.k1,
		B:        idx.b,
		Total:    idx.total,
		Avg:      idx.avg,
		DocLen:   idx.docLen,
		Postings: idx.postings,
		NumDocs:  idx.numDocs,
	})
}

// Load replaces the index contents with a snapshot previously written by Save.
func (idx *BM25Index) Load(r io.Reader) error {
	var s bm25Snapshot
	if err := gob.NewDecoder(r).Decode(&s); err != nil {
		return err
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.k1, idx.b = s.K1, s.B
	if idx.k1 == 0 {
		idx.k1 = 1.2
	}
	if idx.b == 0 {
		idx.b = 0.75
	}
	idx.total, idx.avg, idx.numDocs = s.Total, s.Avg, s.NumDocs
	idx.docLen = s.DocLen
	idx.postings = s.Postings
	if idx.docLen == nil {
		idx.docLen = make(map[string]int)
	}
	if idx.postings == nil {
		idx.postings = make(map[string]map[string]int)
	}
	return nil
}

// tokenize splits text into lowercase alphanumeric tokens.
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return words
}
