package search

import (
	"strings"
	"testing"
)

func runeSlice(s string, sp Span) string {
	r := []rune(s)
	return string(r[sp.Start:sp.End])
}

func TestTokenizeWithPositions(t *testing.T) {
	toks := tokenizeWithPositions("Hello,  world 42!")
	want := []Token{
		{"hello", 0, 5},
		{"world", 8, 13},
		{"42", 14, 16},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i] != w {
			t.Errorf("token %d = %+v, want %+v", i, toks[i], w)
		}
	}
}

func TestTokenizeWithPositionsUnicode(t *testing.T) {
	// "café résumé" — accented runes are single runes, offsets are rune-based.
	toks := tokenizeWithPositions("café résumé")
	if len(toks) != 2 {
		t.Fatalf("got %d tokens: %+v", len(toks), toks)
	}
	if got := runeSlice("café résumé", Span{toks[1].Start, toks[1].End}); got != "résumé" {
		t.Errorf("second token slice = %q, want %q", got, "résumé")
	}
}

func TestIDFRarerTermScoresHigher(t *testing.T) {
	idx := NewBM25Index()
	idx.Add("a", "common common common word")
	idx.Add("b", "common filler text")
	idx.Add("c", "common padding rare")

	common := idx.IDF("common") // in every doc
	rare := idx.IDF("rare")     // in one doc
	if !(rare > common) {
		t.Fatalf("expected rare (%.3f) > common (%.3f)", rare, common)
	}
	if idx.IDF("absent") <= 0 {
		t.Errorf("absent term IDF should be positive, got %.3f", idx.IDF("absent"))
	}
}

func TestLocateCentersOnMatchNotOffsetZero(t *testing.T) {
	filler := strings.Repeat("the meeting covered many routine housekeeping items and administrative notes ", 8)
	chunk := filler + "the budget shortfall forced the layoffs decision " + filler

	res := Locate(chunk, "budget shortfall layoffs", nil, LocateOptions{MaxWindowRunes: 120})
	if !res.Found {
		t.Fatal("expected Found")
	}
	got := runeSlice(chunk, res.Window)
	for _, term := range []string{"budget", "shortfall", "layoffs"} {
		if !strings.Contains(got, term) {
			t.Errorf("window %q missing %q", got, term)
		}
	}
	if res.Window.Start == 0 {
		t.Errorf("window still anchored at offset 0")
	}
	if w := res.Window.End - res.Window.Start; w > 120 {
		t.Errorf("window width %d exceeds max 120", w)
	}
}

func TestLocateNoMatch(t *testing.T) {
	res := Locate("the quick brown fox", "elephant giraffe", nil, LocateOptions{})
	if res.Found {
		t.Errorf("expected not Found, got %+v", res)
	}
}

func TestLocateHitOffsets(t *testing.T) {
	chunk := "alpha beta gamma beta delta"
	res := Locate(chunk, "beta", nil, LocateOptions{})
	if !res.Found || len(res.Hits) != 2 {
		t.Fatalf("expected 2 hits, got %+v", res)
	}
	for _, h := range res.Hits {
		if runeSlice(chunk, h) != "beta" {
			t.Errorf("hit slice = %q, want %q", runeSlice(chunk, h), "beta")
		}
	}
}

func TestLocateIDFWeightingPicksRarerCluster(t *testing.T) {
	idx := NewBM25Index()
	// "the" is everywhere, "quantum" is rare.
	for i := 0; i < 20; i++ {
		idx.Add(string(rune('a'+i)), "the the the the ordinary filler the the")
	}
	idx.Add("q", "the quantum the entanglement the")

	chunk := "the the the the the the the here we discuss quantum entanglement in detail the the the the"
	res := Locate(chunk, "the quantum entanglement", idx.IDF, LocateOptions{MaxWindowRunes: 80})
	got := runeSlice(chunk, res.Window)
	if !strings.Contains(got, "quantum") || !strings.Contains(got, "entanglement") {
		t.Errorf("IDF weighting should center on the rare-term cluster, got %q", got)
	}
}
