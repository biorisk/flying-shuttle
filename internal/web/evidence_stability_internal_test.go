package web

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

func TestEvidenceStability_freezesWindowWhileResultSetHolds(t *testing.T) {
	s := newEvidenceStability()

	first := []viewmodel.Candidate{
		{ChunkID: "c1", Snippet: "…budget shortfall…", FocusStart: 40, FocusEnd: 70},
		{ChunkID: "c2", Snippet: "…the vote…", FocusStart: 10, FocusEnd: 30},
	}
	s.stabilize("n1", first)

	// Same chunks, but the locator (re-run on a longer query) moved the window.
	grown := []viewmodel.Candidate{
		{ChunkID: "c2", Snippet: "…different…", FocusStart: 90, FocusEnd: 120},
		{ChunkID: "c1", Snippet: "…moved…", FocusStart: 0, FocusEnd: 25},
	}
	got := s.stabilize("n1", grown)

	byID := map[string]viewmodel.Candidate{}
	for _, c := range got {
		byID[c.ChunkID] = c
	}
	if byID["c1"].FocusStart != 40 || byID["c1"].Snippet != "…budget shortfall…" {
		t.Errorf("c1 window not frozen: %+v", byID["c1"])
	}
	if byID["c2"].FocusEnd != 30 {
		t.Errorf("c2 window not frozen: %+v", byID["c2"])
	}
	// Ranking still follows the fresh results.
	if got[0].ChunkID != "c2" {
		t.Errorf("expected fresh ordering, got %s first", got[0].ChunkID)
	}
}

func TestEvidenceStability_relocatesWhenResultSetChanges(t *testing.T) {
	s := newEvidenceStability()
	s.stabilize("n1", []viewmodel.Candidate{{ChunkID: "c1", FocusStart: 40, FocusEnd: 70}})

	got := s.stabilize("n1", []viewmodel.Candidate{
		{ChunkID: "c1", FocusStart: 5, FocusEnd: 25},
		{ChunkID: "c9", FocusStart: 0, FocusEnd: 12},
	})
	if got[0].FocusStart != 5 {
		t.Errorf("new result set should use fresh window, got %+v", got[0])
	}
}
