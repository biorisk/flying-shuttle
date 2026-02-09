package stitch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// --- StubStitcher tests ---

func TestStub_empty(t *testing.T) {
	s := &StubStitcher{}
	r, err := s.Stitch(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Spans) != 0 {
		t.Fatalf("expected 0 spans, got %d", len(r.Spans))
	}
	if r.Text != "" {
		t.Fatalf("expected empty text, got %q", r.Text)
	}
}

func TestStub_single(t *testing.T) {
	s := &StubStitcher{}
	r, err := s.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{{ID: "c1", Content: "Hello world"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(r.Spans))
	}
	if r.Spans[0].Type != SpanChunk || r.Spans[0].Text != "Hello world" {
		t.Fatalf("unexpected span: %+v", r.Spans[0])
	}
	if r.Stats.GlueChars != 0 {
		t.Fatalf("expected 0 glue chars, got %d", r.Stats.GlueChars)
	}
}

func TestStub_multiple(t *testing.T) {
	s := &StubStitcher{}
	r, err := s.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{
			{ID: "c1", Content: "First chunk"},
			{ID: "c2", Content: "Second chunk"},
			{ID: "c3", Content: "Third chunk"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 3 chunks + 2 glue spans = 5 total.
	if len(r.Spans) != 5 {
		t.Fatalf("expected 5 spans, got %d", len(r.Spans))
	}
	// All chunk spans should have correct IDs.
	chunkCount := 0
	for _, sp := range r.Spans {
		if sp.Type == SpanChunk {
			chunkCount++
		}
	}
	if chunkCount != 3 {
		t.Fatalf("expected 3 chunk spans, got %d", chunkCount)
	}
	if r.Stats.GlueChars == 0 {
		t.Fatal("expected some glue chars")
	}
	if r.Stats.GlueRatio >= 1.0 || r.Stats.GlueRatio <= 0 {
		t.Fatalf("unexpected glue ratio: %f", r.Stats.GlueRatio)
	}
	expected := "First chunk ... Second chunk ... Third chunk"
	if r.Text != expected {
		t.Fatalf("expected %q, got %q", expected, r.Text)
	}
}

func TestStub_chunkIDs(t *testing.T) {
	s := &StubStitcher{}
	r, err := s.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{
			{ID: "abc", Content: "A"},
			{ID: "def", Content: "B"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Spans[0].ChunkID != "abc" || r.Spans[2].ChunkID != "def" {
		t.Fatalf("chunk IDs not preserved: %+v", r.Spans)
	}
}

// --- PromptStitcher tests ---

// mockCompleter returns a fixed response.
type mockCompleter struct {
	response string
	err      error
}

func (m *mockCompleter) Complete(_ context.Context, _, _ string) (string, error) {
	return m.response, m.err
}

func TestPrompt_singleChunk(t *testing.T) {
	ps := &PromptStitcher{Complete: &mockCompleter{}} // won't be called
	r, err := ps.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{{ID: "c1", Content: "Solo chunk"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Spans) != 1 || r.Spans[0].Text != "Solo chunk" {
		t.Fatalf("unexpected result: %+v", r.Spans)
	}
}

func TestPrompt_parsesLLMResponse(t *testing.T) {
	llmResponse := `[
		{"type":"chunk","index":0,"text":"The hero set out on a journey."},
		{"type":"glue","text":" Along the way, "},
		{"type":"chunk","index":1,"text":"He found an ancient sword."}
	]`

	ps := &PromptStitcher{Complete: &mockCompleter{response: llmResponse}}
	r, err := ps.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{
			{ID: "c1", Content: "The hero set out on a journey."},
			{ID: "c2", Content: "He found an ancient sword."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(r.Spans))
	}
	if r.Spans[0].ChunkID != "c1" || r.Spans[2].ChunkID != "c2" {
		t.Fatalf("chunk IDs wrong: %+v", r.Spans)
	}
	if r.Spans[1].Type != SpanGlue {
		t.Fatal("expected glue span in middle")
	}
	if !strings.Contains(r.Text, "Along the way") {
		t.Fatalf("text missing glue: %q", r.Text)
	}
}

func TestPrompt_stripsCodeFences(t *testing.T) {
	llmResponse := "```json\n" + `[{"type":"chunk","index":0,"text":"A"},{"type":"glue","text":" and "},{"type":"chunk","index":1,"text":"B"}]` + "\n```"

	ps := &PromptStitcher{Complete: &mockCompleter{response: llmResponse}}
	r, err := ps.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{
			{ID: "c1", Content: "A"},
			{ID: "c2", Content: "B"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "A and B" {
		t.Fatalf("expected 'A and B', got %q", r.Text)
	}
}

func TestPrompt_stats(t *testing.T) {
	llmResponse := `[
		{"type":"chunk","index":0,"text":"Hello"},
		{"type":"glue","text":" then "},
		{"type":"chunk","index":1,"text":"World"}
	]`

	ps := &PromptStitcher{Complete: &mockCompleter{response: llmResponse}}
	r, err := ps.Stitch(context.Background(), Request{
		Chunks: []ChunkInput{
			{ID: "c1", Content: "Hello"},
			{ID: "c2", Content: "World"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.ChunkChars != 10 { // "Hello" + "World"
		t.Fatalf("expected 10 chunk chars, got %d", r.Stats.ChunkChars)
	}
	if r.Stats.GlueChars != 6 { // " then "
		t.Fatalf("expected 6 glue chars, got %d", r.Stats.GlueChars)
	}
	if r.Stats.TotalChars != 16 {
		t.Fatalf("expected 16 total chars, got %d", r.Stats.TotalChars)
	}
}

func TestPrompt_glueZero(t *testing.T) {
	// With GlueLevel explicitly set to -1 (force zero), the user prompt
	// should include the "no transition" instruction.
	var capturedUser string
	ps := &PromptStitcher{Complete: &mockCompleter{
		response: `[{"type":"chunk","index":0,"text":"A"},{"type":"chunk","index":1,"text":"B"}]`,
	}}
	// Override the completer to capture the prompt.
	ps.Complete = &promptCapture{
		response: `[{"type":"chunk","index":0,"text":"A"},{"type":"chunk","index":1,"text":"B"}]`,
		capture:  &capturedUser,
	}

	_, err := ps.Stitch(context.Background(), Request{
		Chunks:    []ChunkInput{{ID: "c1", Content: "A"}, {ID: "c2", Content: "B"}},
		GlueLevel: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedUser, "NO transition") {
		t.Fatalf("expected zero-glue instruction, got: %s", capturedUser)
	}
}

type promptCapture struct {
	response string
	capture  *string
}

func (p *promptCapture) Complete(_ context.Context, _, user string) (string, error) {
	*p.capture = user
	return p.response, nil
}

// --- parseSpans tests ---

func TestParseSpans_invalidJSON(t *testing.T) {
	_, err := parseSpans("not json", nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseSpans_unknownType(t *testing.T) {
	_, err := parseSpans(`[{"type":"unknown","text":"x"}]`, nil)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

// --- buildResult tests ---

func TestBuildResult_emptySpans(t *testing.T) {
	r := buildResult(nil)
	if r.Text != "" || r.Stats.TotalChars != 0 || r.Stats.GlueRatio != 0 {
		t.Fatalf("unexpected empty result: %+v", r)
	}
}

// --- glueInstruction tests ---

func TestGlueInstruction_levels(t *testing.T) {
	tests := []struct {
		level    int
		contains string
	}{
		{0, "NO transition"},
		{10, "conjunctions"},
		{50, "transitional phrases"},
		{75, "transitional sentences"},
		{100, "freely"},
	}
	for _, tt := range tests {
		got := glueInstruction(tt.level)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("glueInstruction(%d) = %q, want substring %q", tt.level, got, tt.contains)
		}
	}
}

// --- JSON serialization ---

func TestSpan_JSON(t *testing.T) {
	s := Span{Type: SpanChunk, ChunkIndex: 2, ChunkID: "abc", Text: "hello"}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Span
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ChunkID != "abc" || decoded.Type != SpanChunk {
		t.Fatalf("roundtrip failed: %+v", decoded)
	}
}
