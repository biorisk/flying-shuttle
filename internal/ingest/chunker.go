package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

// ChunkerConfig controls topic-shift-based chunking.
type ChunkerConfig struct {
	// Threshold is the cosine distance (1 - similarity) above which a chunk
	// boundary is inserted between adjacent sentences. Range [0, 2].
	// Default: 0.5
	Threshold float64

	// MinSentences is the minimum number of sentences per chunk.
	// Prevents tiny chunks from single-sentence topic blips.
	// Default: 2
	MinSentences int
}

func (c *ChunkerConfig) threshold() float64 {
	if c.Threshold > 0 {
		return c.Threshold
	}
	return 0.5
}

func (c *ChunkerConfig) minSentences() int {
	if c.MinSentences > 0 {
		return c.MinSentences
	}
	return 2
}

// Chunker splits transcript segments into semantic chunks using embedding
// distance to detect topic boundaries.
type Chunker struct {
	Embedder Embedder
	Config   ChunkerConfig
}

// ChunkSegments takes an ordered list of transcript segments from a single
// upload and returns immutable Chunk records grouped by topic similarity.
// Each chunk includes the concatenated text, speaker (from the first segment
// in the group), and the timestamp range spanning all segments in the chunk.
func (c *Chunker) ChunkSegments(ctx context.Context, sourceFile string, segments []model.TranscriptSegment) ([]model.Chunk, error) {
	if len(segments) == 0 {
		return nil, nil
	}

	// Extract sentence texts for embedding.
	texts := make([]string, len(segments))
	for i, seg := range segments {
		texts[i] = seg.Text
	}

	embeddings, err := c.Embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed batch: %w", err)
	}

	// Find boundary indices where cosine distance exceeds threshold.
	threshold := c.Config.threshold()
	minSent := c.Config.minSentences()
	boundaries := findBoundaries(embeddings, threshold, minSent)

	// Split segments at boundaries into chunks.
	chunks := buildChunks(sourceFile, segments, embeddings, boundaries)
	return chunks, nil
}

// findBoundaries returns segment indices where a new chunk should begin.
// Index 0 is always a boundary (start of first chunk).
func findBoundaries(embeddings [][]float32, threshold float64, minSentences int) []int {
	boundaries := []int{0}
	lastBoundary := 0

	for i := 1; i < len(embeddings); i++ {
		dist := 1.0 - CosineSimilarity(embeddings[i-1], embeddings[i])
		chunkSize := i - lastBoundary // sentences in current chunk before the split

		if dist > threshold && chunkSize >= minSentences {
			boundaries = append(boundaries, i)
			lastBoundary = i
		}
	}
	return boundaries
}

// buildChunks creates Chunk records from segments split at the given boundaries.
func buildChunks(sourceFile string, segments []model.TranscriptSegment, embeddings [][]float32, boundaries []int) []model.Chunk {
	var chunks []model.Chunk

	for b := 0; b < len(boundaries); b++ {
		start := boundaries[b]
		end := len(segments)
		if b+1 < len(boundaries) {
			end = boundaries[b+1]
		}

		group := segments[start:end]
		if len(group) == 0 {
			continue
		}

		// Concatenate text.
		var textParts []string
		for _, seg := range group {
			textParts = append(textParts, seg.Text)
		}

		// Speaker: use first segment's speaker.
		var speaker *string
		if group[0].Speaker != "" {
			s := group[0].Speaker
			speaker = &s
		}

		// Timestamp range.
		startMs := group[0].StartMs
		endMs := group[len(group)-1].EndMs

		// Average embedding for the chunk.
		avgEmb := averageEmbeddings(embeddings[start:end])

		chunks = append(chunks, model.Chunk{
			ID:           uuid.NewString(),
			SourceFile:   sourceFile,
			Content:      strings.Join(textParts, " "),
			StartOffset:  int(startMs),
			EndOffset:    int(endMs),
			Speaker:      speaker,
			EmbeddingVec: Float32sToBytes(avgEmb),
		})
	}

	return chunks
}

// averageEmbeddings computes the mean vector of a set of embeddings.
func averageEmbeddings(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	avg := make([]float32, dim)
	for _, v := range vecs {
		for i := range avg {
			if i < len(v) {
				avg[i] += v[i]
			}
		}
	}
	n := float32(len(vecs))
	for i := range avg {
		avg[i] /= n
	}
	return avg
}
