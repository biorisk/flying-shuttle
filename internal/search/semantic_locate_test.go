package search

import (
	"context"
	"strings"
	"testing"
)

// bagEmbedder embeds text as a bag-of-words vector over a fixed vocabulary, so
// texts sharing vocabulary score high on cosine similarity — enough to drive
// SemanticLocate deterministically without a real model.
type bagEmbedder struct{ vocab []string }

func (b bagEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vs, err := b.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

func (b bagEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		lt := strings.ToLower(t)
		v := make([]float32, len(b.vocab))
		for j, w := range b.vocab {
			v[j] = float32(strings.Count(lt, w))
		}
		out[i] = v
	}
	return out, nil
}

func TestSemanticLocatePicksSimilarSentence(t *testing.T) {
	emb := bagEmbedder{vocab: []string{"revenue", "profit", "quarter", "weather", "coffee", "parking"}}
	chunk := "The weather was miserable all week. " +
		"Revenue and profit both climbed for the third quarter running. " +
		"We argued about parking again."

	res := SemanticLocate(context.Background(), emb, chunk, "how did earnings do this quarter", LocateOptions{MaxWindowRunes: 240})
	if !res.Found {
		t.Fatal("expected Found")
	}
	got := runeSlice(chunk, res.Window)
	if !strings.Contains(got, "Revenue and profit both climbed") {
		t.Errorf("semantic window missed the earnings sentence: %q", got)
	}
	if len(res.Sentences) != 3 {
		t.Fatalf("expected 3 sentence scores, got %d", len(res.Sentences))
	}
	if res.Sentences[1].Score != 1.0 {
		t.Errorf("earnings sentence should normalize to 1.0, got %v", res.Sentences[1].Score)
	}
	if len(res.Hits) != 0 {
		t.Errorf("semantic locate should not produce lexical hits, got %d", len(res.Hits))
	}
}

func TestSemanticLocateNilEmbedderAndLowSim(t *testing.T) {
	if SemanticLocate(context.Background(), nil, "a. b.", "q", LocateOptions{}).Found {
		t.Error("nil embedder must yield not-Found")
	}
	emb := bagEmbedder{vocab: []string{"zzz"}}
	if SemanticLocate(context.Background(), emb, "nothing matches here. still nothing.", "zzz", LocateOptions{MinSimilarity: 0.9}).Found {
		t.Error("below MinSimilarity must yield not-Found")
	}
}
