package atlas

import (
	"context"
	"slices"
	"testing"
)

// A small synthetic corpus: three chunks about sailing, two about baking.
var corpus = []string{
	"The mainsail luffed as the skipper brought the bow through the wind, tacking hard.",
	"We trimmed the jib and the sloop heeled over, spray coming off the windward rail.",
	"Reefing the mainsail early kept the boat flat when the squall finally hit us.",
	"She folded the butter into the flour until the dough just came together, then chilled it.",
	"The sourdough needs a long cold proof so the crumb opens up and the crust blisters.",
}

func TestKeyworder_Top(t *testing.T) {
	kw := NewKeyworder(corpus)

	got := kw.Top(corpus[0], 3)
	// "mainsail", "skipper", "tacking", "luffed", "bow" are all rare (df==1);
	// each appears once, so the tie-break is alphabetical.
	if len(got) != 3 {
		t.Fatalf("want 3 terms, got %v", got)
	}
	for _, term := range got {
		if term == "the" || term == "as" || term == "and" {
			t.Fatalf("stopword leaked into keywords: %v", got)
		}
	}

	// A term shared across the sailing chunks ("mainsail") should outrank a
	// one-off when we aggregate over that region's chunks.
	sailing := kw.TopFromDocs(corpus[:3], 5)
	if !slices.Contains(sailing, "mainsail") {
		t.Fatalf("expected 'mainsail' in aggregated sailing keywords: %v", sailing)
	}
	if slices.Contains(sailing, "butter") || slices.Contains(sailing, "sourdough") {
		t.Fatalf("baking terms leaked into sailing region: %v", sailing)
	}
}

func TestKeyworder_Deterministic(t *testing.T) {
	kw := NewKeyworder(corpus)
	a := kw.TopFromDocs(corpus[:3], 6)
	b := kw.TopFromDocs(corpus[:3], 6)
	if !slices.Equal(a, b) {
		t.Fatalf("non-deterministic: %v vs %v", a, b)
	}
}

func TestExtractiveSummariser(t *testing.T) {
	kw := NewKeyworder(corpus)
	s := &ExtractiveSummariser{KW: kw}

	d, err := s.Summarise(context.Background(), SummariseInput{Texts: corpus[:3]})
	if err != nil {
		t.Fatalf("Summarise: %v", err)
	}
	if d.Source != "extractive" {
		t.Fatalf("source = %q", d.Source)
	}
	if d.Title == "" || len(d.Keywords) == 0 {
		t.Fatalf("empty digest: %+v", d)
	}
	if d.Abstract == "" {
		t.Fatalf("empty abstract")
	}
	// Abstract is drawn from the first (centroid-nearest) chunk.
	if d.Abstract != "The mainsail luffed as the skipper brought the bow through the wind, tacking hard." {
		t.Fatalf("abstract not the leading sentence of Texts[0]: %q", d.Abstract)
	}
	// Title is title-cased top terms joined with " · ".
	if r := []rune(d.Title); r[0] < 'A' || r[0] > 'Z' {
		t.Fatalf("title not capitalised: %q", d.Title)
	}

	// Stable across calls.
	d2, _ := s.Summarise(context.Background(), SummariseInput{Texts: corpus[:3]})
	if d2.Title != d.Title || !slices.Equal(d2.Keywords, d.Keywords) {
		t.Fatalf("non-deterministic digest")
	}
}

func TestFirstSentences(t *testing.T) {
	got := firstSentences("One two. Three four! Five six? Seven.", 2)
	if got != "One two. Three four!" {
		t.Fatalf("got %q", got)
	}
	got = firstSentences("no punctuation here just words", 3)
	if got != "no punctuation here just words" {
		t.Fatalf("fallback failed: %q", got)
	}
}
