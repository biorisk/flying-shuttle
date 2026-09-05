package atlas

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type scriptCompleter struct {
	reply string
	err   error
	calls int
}

func (c *scriptCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	c.calls++
	return c.reply, c.err
}

func TestChunkLabeller_ParsesAndFallsBack(t *testing.T) {
	// Model labels 1 and 3, mangles 2, drops 4.
	cmp := &scriptCompleter{reply: "1. tacking upwind\n2. \n3) reefing the mainsail\n"}
	l := &ChunkLabeller{Complete: cmp, ModelName: "gemma", Batch: 10, MaxWords: 6}

	in := []LabelInput{
		{ChunkID: "a", Text: "we came about and tacked hard upwind"},
		{ChunkID: "b", Text: "then the wind dropped completely for a while"},
		{ChunkID: "c", Text: "put a reef in the main before the gust"},
		{ChunkID: "d", Text: "the tide turned against us near the point"},
	}

	var got []ChunkLabel
	err := l.Label(context.Background(), in, func(batch []ChunkLabel) error {
		got = append(got, batch...)
		return nil
	})
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 labels, got %d: %+v", len(got), got)
	}
	if got[0].Label != "tacking upwind" || got[0].Source != "llm:gemma" {
		t.Fatalf("a: %+v", got[0])
	}
	if got[2].Label != "reefing the mainsail" || got[2].Source != "llm:gemma" {
		t.Fatalf("c: %+v", got[2])
	}
	// b (mangled) and d (dropped) fall back to a text head.
	for _, i := range []int{1, 3} {
		if got[i].Source != "head" || got[i].Label == "" {
			t.Fatalf("index %d should be a head fallback: %+v", i, got[i])
		}
		if strings.Fields(got[i].Label)[0] != strings.Fields(in[i].Text)[0] {
			t.Fatalf("index %d head label doesn't start at the text head: %q", i, got[i].Label)
		}
	}
}

func TestChunkLabeller_BatchesAndPersistsPerBatch(t *testing.T) {
	cmp := &scriptCompleter{reply: "1. one\n2. two\n3. three\n"}
	l := &ChunkLabeller{Complete: cmp, Batch: 2}

	in := []LabelInput{{ChunkID: "a", Text: "aa"}, {ChunkID: "b", Text: "bb"}, {ChunkID: "c", Text: "cc"}}
	persists := 0
	err := l.Label(context.Background(), in, func([]ChunkLabel) error { persists++; return nil })
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if cmp.calls != 2 || persists != 2 { // 3 items, batch 2 -> 2 calls, 2 persists
		t.Fatalf("calls=%d persists=%d, want 2 and 2", cmp.calls, persists)
	}
}

func TestChunkLabeller_ModelErrorStopsWithoutPersisting(t *testing.T) {
	cmp := &scriptCompleter{err: errors.New("llm down")}
	l := &ChunkLabeller{Complete: cmp, Batch: 2}

	persists := 0
	err := l.Label(context.Background(), []LabelInput{{ChunkID: "a"}, {ChunkID: "b"}},
		func([]ChunkLabel) error { persists++; return nil })
	if err == nil {
		t.Fatal("want an error when the model call fails")
	}
	if persists != 0 {
		t.Fatalf("nothing should persist on a failed batch, got %d", persists)
	}
}
