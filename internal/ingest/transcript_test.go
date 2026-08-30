package ingest

import (
	"strings"
	"testing"
)

func TestParseTranscript_paragraphsAndSentences(t *testing.T) {
	raw := "First sentence. Second sentence here.\n\nA new paragraph starts. It has two sentences.\n"
	segs := ParseTranscript(raw)
	if len(segs) != 4 {
		t.Fatalf("expected 4 segments, got %d: %#v", len(segs), segs)
	}
	if segs[0].Text != "First sentence." {
		t.Errorf("segment 0 = %q", segs[0].Text)
	}
	if segs[2].Text != "A new paragraph starts." {
		t.Errorf("segment 2 = %q", segs[2].Text)
	}
}

func TestParseTranscript_collapsesWhitespace(t *testing.T) {
	segs := ParseTranscript("line one\n   line two   with   gaps")
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Text != "line one line two with gaps" {
		t.Errorf("got %q", segs[0].Text)
	}
}

func TestParseTranscript_empty(t *testing.T) {
	if segs := ParseTranscript("   \n\n  \n"); len(segs) != 0 {
		t.Fatalf("expected no segments, got %#v", segs)
	}
}

func TestChunkTranscript_groupsToTargetSize(t *testing.T) {
	var segs []string
	// 40 segments of 10 words each = 400 words → ~3 chunks at 160 words.
	tenWords := "one two three four five six seven eight nine ten"
	for i := 0; i < 40; i++ {
		segs = append(segs, tenWords)
	}
	parsed := ParseTranscript(strings.Join(segs, "\n\n"))
	chunks := ChunkTranscript("notes.txt", parsed)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.SourceFile != "notes.txt" {
			t.Errorf("chunk %d source = %q", i, c.SourceFile)
		}
		if c.Content == "" {
			t.Errorf("chunk %d empty", i)
		}
		if len(c.EmbeddingVec) != 0 {
			t.Errorf("chunk %d should have no embedding", i)
		}
	}
	if chunks[0].EndOffset <= chunks[0].StartOffset {
		t.Errorf("chunk 0 offsets: %d..%d", chunks[0].StartOffset, chunks[0].EndOffset)
	}
	if chunks[1].StartOffset < chunks[0].EndOffset {
		t.Errorf("chunk offsets overlap: %d then %d", chunks[0].EndOffset, chunks[1].StartOffset)
	}
}

func TestChunkTranscript_empty(t *testing.T) {
	if chunks := ChunkTranscript("x.txt", nil); chunks != nil {
		t.Fatalf("expected nil, got %#v", chunks)
	}
}

func TestIsTextTranscript(t *testing.T) {
	for _, ok := range []string{".txt", ".TXT", ".md", ".markdown", ".text"} {
		if !IsTextTranscript(ok) {
			t.Errorf("%s should be a transcript format", ok)
		}
	}
	for _, no := range []string{".mp3", ".wav", ".pdf", ""} {
		if IsTextTranscript(no) {
			t.Errorf("%s should not be a transcript format", no)
		}
	}
}
