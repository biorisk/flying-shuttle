package main

import (
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
)

func TestExcerptDiverges(t *testing.T) {
	chunk := "ALPHA the quick brown fox jumps OMEGA"
	cases := []struct {
		name string
		ev   model.Evidence
		want bool
	}{
		{"exact slice", model.Evidence{CharStart: 6, CharEnd: 31, Text: "the quick brown fox jumps"}, false},
		{"stale text", model.Evidence{CharStart: 6, CharEnd: 31, Text: "the SLOW brown fox jumps"}, true},
		{"out-of-range offsets, text equals whole chunk", model.Evidence{CharStart: 0, CharEnd: 999, Text: chunk}, false},
		{"out-of-range offsets, text differs", model.Evidence{CharStart: 0, CharEnd: 999, Text: "something else"}, true},
	}
	for _, c := range cases {
		if got := excerptDiverges(c.ev, chunk); got != c.want {
			t.Errorf("%s: excerptDiverges = %v, want %v", c.name, got, c.want)
		}
	}
}
