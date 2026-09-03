package search

import (
	"context"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/model"
)

func TestPassageIDRoundTrip(t *testing.T) {
	id := PassageID("chunk-abc", Span{40, 310})
	cid, sp, ok := SplitPassageID(id)
	if !ok || cid != "chunk-abc" || sp != (Span{40, 310}) {
		t.Fatalf("round trip failed: %q -> %q %+v %v", id, cid, sp, ok)
	}
	if _, _, ok := SplitPassageID("no-seps"); ok {
		t.Error("expected failure on malformed id")
	}
}

func TestChunkPassagesOverlapAndCover(t *testing.T) {
	words := make([]string, 200)
	for i := range words {
		words[i] = "w" + string(rune('a'+i%26))
	}
	content := strings.Join(words, " ")
	ps := chunkPassages(content)
	if len(ps) < 3 {
		t.Fatalf("expected several passages, got %d", len(ps))
	}
	if ps[0].Start != 0 {
		t.Errorf("first passage should start at 0, got %d", ps[0].Start)
	}
	last := ps[len(ps)-1]
	if last.End != len([]rune(content)) {
		t.Errorf("last passage should reach the end (%d), got %d", len([]rune(content)), last.End)
	}
	// Adjacent passages overlap.
	if ps[1].Start >= ps[0].End {
		t.Errorf("passages 0 and 1 do not overlap: %+v %+v", ps[0].Span, ps[1].Span)
	}
	for _, p := range ps {
		if p.Text != string([]rune(content)[p.Start:p.End]) {
			t.Errorf("passage text/span mismatch at %+v", p.Span)
		}
	}
}

func TestHybridSearchPassageArmLocatesLateTerm(t *testing.T) {
	filler := strings.Repeat("we discussed logistics and scheduling and the venue at length ", 12)
	content := filler + "the compliance audit findings were severe " + filler

	idx := NewHybridIndex(nil)
	idx.IndexChunk(&model.Chunk{ID: "c1", SourceFile: "f.txt", Content: content})
	idx.IndexChunk(&model.Chunk{ID: "c2", SourceFile: "f.txt", Content: "unrelated chatter about parking permits"})

	res, err := idx.Search(context.Background(), "compliance audit", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 || res[0].ChunkID != "c1" {
		t.Fatalf("expected c1 first, got %+v", res)
	}
	p := res[0].Passage
	if p.End <= p.Start {
		t.Fatalf("expected a passage span on the top result, got %+v", p)
	}
	seg := string([]rune(content)[p.Start:p.End])
	if !strings.Contains(seg, "compliance audit findings") {
		t.Errorf("passage span %+v (%q) does not cover the matched text", p, seg)
	}
	if p.Start == 0 {
		t.Errorf("passage should not start at chunk head")
	}
}
