package search

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// Cluster represents a group of semantically related chunks.
type Cluster struct {
	Label      string   `json:"label"`
	ChunkIDs   []string `json:"chunk_ids"`
	ChunkCount int      `json:"chunk_count"`
	Confidence float64  `json:"confidence"`
}

// ChunkClusterer groups retrieved chunks into sub-themes.
type ChunkClusterer interface {
	Cluster(ctx context.Context, nodeTitle string, chunks []ChunkWithContent) ([]Cluster, error)
}

// ChunkWithContent pairs a chunk ID with its text content for clustering.
type ChunkWithContent struct {
	ID      string
	Content string
	Score   float64 // retrieval score from the search
}

// --- Embedding-based stub clusterer (no LLM needed) ---

// EmbeddingClusterer groups chunks by cosine similarity of their embeddings
// using a simple greedy approach. No LLM required.
type EmbeddingClusterer struct {
	Embedder      ingest.Embedder
	MaxClusters   int     // target number of clusters (default 4)
	MinSimilarity float64 // threshold to join a cluster (default 0.3)
}

func (ec *EmbeddingClusterer) maxClusters() int {
	if ec.MaxClusters > 0 {
		return ec.MaxClusters
	}
	return 4
}

func (ec *EmbeddingClusterer) minSim() float64 {
	if ec.MinSimilarity > 0 {
		return ec.MinSimilarity
	}
	return 0.3
}

func (ec *EmbeddingClusterer) Cluster(ctx context.Context, nodeTitle string, chunks []ChunkWithContent) ([]Cluster, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) == 1 {
		return []Cluster{{
			Label:      labelFromContent(nodeTitle, chunks[0].Content, 0),
			ChunkIDs:   []string{chunks[0].ID},
			ChunkCount: 1,
			Confidence: chunks[0].Score,
		}}, nil
	}

	// Embed all chunks.
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embeddings, err := ec.Embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed chunks for clustering: %w", err)
	}

	// Greedy clustering: assign each chunk to the most similar existing
	// cluster centroid, or start a new cluster.
	type cluster struct {
		ids      []string
		centroid []float32
		scores   []float64
	}

	maxK := ec.maxClusters()
	minSim := ec.minSim()
	var clusters []cluster

	for i, emb := range embeddings {
		bestIdx := -1
		bestSim := -1.0

		for j, cl := range clusters {
			sim := ingest.CosineSimilarity(emb, cl.centroid)
			if sim > bestSim {
				bestSim = sim
				bestIdx = j
			}
		}

		if bestIdx >= 0 && bestSim >= minSim {
			// Add to existing cluster and update centroid (running average).
			cl := &clusters[bestIdx]
			n := float32(len(cl.ids))
			for k := range cl.centroid {
				cl.centroid[k] = (cl.centroid[k]*n + emb[k]) / (n + 1)
			}
			cl.ids = append(cl.ids, chunks[i].ID)
			cl.scores = append(cl.scores, chunks[i].Score)
		} else if len(clusters) < maxK {
			// Start a new cluster.
			centroid := make([]float32, len(emb))
			copy(centroid, emb)
			clusters = append(clusters, cluster{
				ids:      []string{chunks[i].ID},
				centroid: centroid,
				scores:   []float64{chunks[i].Score},
			})
		} else {
			// Force into most similar existing cluster.
			if bestIdx >= 0 {
				cl := &clusters[bestIdx]
				n := float32(len(cl.ids))
				for k := range cl.centroid {
					cl.centroid[k] = (cl.centroid[k]*n + emb[k]) / (n + 1)
				}
				cl.ids = append(cl.ids, chunks[i].ID)
				cl.scores = append(cl.scores, chunks[i].Score)
			}
		}
	}

	// Convert to output format.
	result := make([]Cluster, len(clusters))
	for i, cl := range clusters {
		avgScore := 0.0
		for _, s := range cl.scores {
			avgScore += s
		}
		avgScore /= float64(len(cl.scores))

		// Find representative chunk (highest score) for label generation.
		bestChunkIdx := 0
		for j, id := range cl.ids {
			if id == cl.ids[0] {
				bestChunkIdx = j
			}
			if cl.scores[j] > cl.scores[bestChunkIdx] {
				bestChunkIdx = j
			}
		}

		// Find the representative content.
		var repContent string
		for _, c := range chunks {
			if c.ID == cl.ids[bestChunkIdx] {
				repContent = c.Content
				break
			}
		}

		result[i] = Cluster{
			Label:      labelFromContent(nodeTitle, repContent, i),
			ChunkIDs:   cl.ids,
			ChunkCount: len(cl.ids),
			Confidence: math.Min(avgScore, 1.0),
		}
	}

	return result, nil
}

// labelFromContent generates a descriptive sub-theme label.
func labelFromContent(nodeTitle, content string, index int) string {
	themes := []string{"key evidence", "emotional context", "counterpoint", "background"}
	if index < len(themes) {
		return nodeTitle + " — " + themes[index]
	}
	return nodeTitle + " — " + fmt.Sprintf("theme %d", index+1)
}

// --- LLM-based clusterer ---

// LLMClusterer uses an LLM to identify sub-themes from chunks.
type LLMClusterer struct {
	Complete Completer
}

// Completer sends a prompt to an LLM (shared interface with stitch package).
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

const clusterSystemPrompt = `You are a text analysis assistant. Given a bullet point title and a set of transcript chunks, identify 3-4 coherent sub-themes that the chunks cluster into.

Output a JSON array of objects:
[
  {"label": "<short descriptive theme name>", "chunk_indices": [0, 2, 5], "confidence": 0.85}
]

Rules:
- Each chunk index should appear in exactly one cluster
- Labels should be short (2-5 words) and descriptive
- Confidence is 0-1, higher for tighter thematic coherence
- Return ONLY the JSON array, no commentary`

func (lc *LLMClusterer) Cluster(ctx context.Context, nodeTitle string, chunks []ChunkWithContent) ([]Cluster, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Bullet point: %s\n\nChunks:\n\n", nodeTitle)
	for i, c := range chunks {
		fmt.Fprintf(&b, "--- Chunk %d ---\n%s\n\n", i, c.Content)
	}

	raw, err := lc.Complete.Complete(ctx, clusterSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("cluster LLM call: %w", err)
	}

	return parseClusters(raw, nodeTitle, chunks)
}

type rawCluster struct {
	Label        string  `json:"label"`
	ChunkIndices []int   `json:"chunk_indices"`
	Confidence   float64 `json:"confidence"`
}

func parseClusters(raw, nodeTitle string, chunks []ChunkWithContent) ([]Cluster, error) {
	raw = strings.TrimSpace(raw)
	// Strip code fences.
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) >= 2 {
			lines = lines[1:]
			if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				lines = lines[:len(lines)-1]
			}
		}
		raw = strings.Join(lines, "\n")
	}

	var raws []rawCluster
	if err := json.Unmarshal([]byte(raw), &raws); err != nil {
		return nil, fmt.Errorf("invalid cluster JSON: %w", err)
	}

	result := make([]Cluster, 0, len(raws))
	for _, rc := range raws {
		ids := make([]string, 0, len(rc.ChunkIndices))
		for _, idx := range rc.ChunkIndices {
			if idx >= 0 && idx < len(chunks) {
				ids = append(ids, chunks[idx].ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		label := rc.Label
		if label == "" {
			label = nodeTitle
		}
		result = append(result, Cluster{
			Label:      nodeTitle + " — " + label,
			ChunkIDs:   ids,
			ChunkCount: len(ids),
			Confidence: rc.Confidence,
		})
	}

	return result, nil
}
