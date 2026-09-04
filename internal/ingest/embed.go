package ingest

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand"
)

// Embedder computes dense vector embeddings for text.
// QueryEmbedder is an optional extension: an Embedder that distinguishes the
// query side of an asymmetric retrieval model from the document side. Search
// code should type-assert for it and fall back to Embed when absent.
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// EmbedQueryOr uses e.EmbedQuery when e implements QueryEmbedder, else e.Embed.
func EmbedQueryOr(ctx context.Context, e Embedder, text string) ([]float32, error) {
	if qe, ok := e.(QueryEmbedder); ok {
		return qe.EmbedQuery(ctx, text)
	}
	return e.Embed(ctx, text)
}

type Embedder interface {
	// Embed returns a float32 embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch returns embeddings for multiple texts. Default
	// implementations may call Embed in a loop.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// StubEmbedder returns deterministic pseudo-random 64-dim embeddings.
// Seeded by a hash of the input text so identical strings produce identical vectors.
// Replace with a real provider (e.g. qwen3-embedding, OpenAI) when ready.
type StubEmbedder struct {
	Dim int // embedding dimension; 0 defaults to 64
}

func (e *StubEmbedder) dim() int {
	if e.Dim > 0 {
		return e.Dim
	}
	return 64
}

func (e *StubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	// Simple deterministic seed from text.
	var seed int64
	for _, r := range text {
		seed = seed*31 + int64(r)
	}
	rng := rand.New(rand.NewSource(seed))
	d := e.dim()
	vec := make([]float32, d)
	var norm float64
	for i := range vec {
		v := float32(rng.NormFloat64())
		vec[i] = v
		norm += float64(v * v)
	}
	// L2 normalize.
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= float32(norm)
		}
	}
	return vec, nil
}

func (e *StubEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := e.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// Float32sToBytes serializes a float32 slice to little-endian bytes
// for storage in the chunk embedding_vec column.
func Float32sToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// BytesToFloat32s deserializes bytes back to a float32 slice.
func BytesToFloat32s(b []byte) []float32 {
	n := len(b) / 4
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// CosineSimilarity computes cosine similarity between two vectors.
// Returns 0 if either vector is zero.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
