package search

import (
	"bufio"
	"io"
	"os"
	"sync"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/coder/hnsw"
)

// VectorIndex is an HNSW-backed approximate nearest-neighbour search index.
// It stores pre-computed embeddings keyed by chunk ID.
// Query-time embedding is not performed here; call Search with a pre-computed
// vector, or skip vector search entirely by using HybridIndex with a nil Embedder.
//
// All methods are safe for concurrent use.
type VectorIndex struct {
	mu    sync.RWMutex
	graph *hnsw.Graph[string]
}

// NewVectorIndex creates an empty HNSW vector index with cosine distance.
func NewVectorIndex() *VectorIndex {
	return &VectorIndex{graph: hnsw.NewGraph[string]()}
}

// Add inserts (or replaces) a chunk's embedding vector in the index.
func (idx *VectorIndex) Add(id string, vec []float32) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.graph.Add(hnsw.MakeNode(id, vec))
}

// Search finds the k approximate nearest neighbours to vec.
// Results are scored by cosine similarity (higher = more similar).
// Returns nil if the index is empty.
func (idx *VectorIndex) Search(vec []float32, limit int) []Result {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if idx.graph.Len() == 0 {
		return nil
	}
	nodes := idx.graph.Search(vec, limit)
	results := make([]Result, len(nodes))
	for i, n := range nodes {
		results[i] = Result{
			ChunkID: n.Key,
			Score:   ingest.CosineSimilarity(vec, n.Value),
		}
	}
	return results
}

// Export writes the HNSW graph to w.
func (idx *VectorIndex) Export(w io.Writer) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.graph.Export(w)
}

// Save persists the HNSW graph to a file at path.
func (idx *VectorIndex) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if err := idx.Export(w); err != nil {
		return err
	}
	return w.Flush()
}

// Load restores the HNSW graph from a file at path.
// Replaces any previously indexed data.
func (idx *VectorIndex) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.graph.Import(bufio.NewReader(f))
}

// Len returns the number of vectors currently indexed.
func (idx *VectorIndex) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.graph.Len()
}

// Has reports whether a chunk ID has a vector in the index.
func (idx *VectorIndex) Has(id string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.graph.Lookup(id)
	return ok
}
