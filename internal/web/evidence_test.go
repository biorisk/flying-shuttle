package web_test

import (
	"context"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
)

func TestEvidenceFinder_ranksAndResolves(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	idx := search.NewHybridIndex(nil) // BM25-only
	chunks := []model.Chunk{
		{ID: "c1", SourceFile: "iv.txt", Content: "I felt a deep fear before the vote"},
		{ID: "c2", SourceFile: "iv.txt", Content: "the weather that morning was clear and cold"},
		{ID: "c3", SourceFile: "iv.txt", Content: "fear gave way to resolve once I started speaking"},
	}
	for i := range chunks {
		if err := s.CreateChunk(&chunks[i]); err != nil {
			t.Fatal(err)
		}
		idx.IndexChunk(&chunks[i])
	}

	f := &web.EvidenceFinder{Index: idx, Store: s}

	got, err := f.Find(context.Background(), "fear", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected the two fear passages, got %d: %+v", len(got), got)
	}
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.ChunkID] = true
		if c.SourceFile != "iv.txt" {
			t.Fatalf("source not resolved: %+v", c)
		}
		if c.Snippet == "" {
			t.Fatalf("snippet empty: %+v", c)
		}
	}
	if !ids["c1"] || !ids["c3"] {
		t.Fatalf("expected c1 and c3, got %v", ids)
	}
	if ids["c2"] {
		t.Fatalf("c2 (no 'fear') should not rank")
	}

	// Blank query -> nil.
	if got, _ := f.Find(context.Background(), "   ", 5); got != nil {
		t.Fatalf("blank query should yield nil, got %v", got)
	}
}

func TestEvidenceFinder_marksHitsAndCentersSnippet(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	idx := search.NewHybridIndex(nil)
	lead := "we spent the first hour on scheduling and room bookings and other logistics "
	c := model.Chunk{
		ID:         "c1",
		SourceFile: "iv.txt",
		Content:    lead + lead + "then the budget shortfall came up and dominated the rest " + lead,
	}
	if err := s.CreateChunk(&c); err != nil {
		t.Fatal(err)
	}
	idx.IndexChunk(&c)

	f := &web.EvidenceFinder{Index: idx, Store: s}
	got, err := f.Find(context.Background(), "budget shortfall", 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("Find: %v / %d results", err, len(got))
	}
	cand := got[0]
	if cand.FocusStart == 0 {
		t.Errorf("snippet still anchored at chunk start: %+v", cand)
	}
	var marked []string
	for _, seg := range cand.Segments {
		if seg.Mark {
			marked = append(marked, seg.Text)
		}
	}
	if len(marked) != 2 {
		t.Fatalf("expected budget + shortfall marked, got %v (segments %+v)", marked, cand.Segments)
	}
	// The clipped snippet offers an expand-to-full-chunk view.
	if !cand.HasMore() {
		t.Errorf("expected HasMore for a clipped snippet")
	}
	var full string
	for _, seg := range cand.Full {
		full += seg.Text
	}
	if full != c.Content {
		t.Errorf("Full segments should reconstruct the whole chunk:\n got %q\nwant %q", full, c.Content)
	}
}
