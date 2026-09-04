package atlas

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubCompleter struct {
	reply string
	err   error
	gotU  string
}

func (s *stubCompleter) Complete(_ context.Context, _, user string) (string, error) {
	s.gotU = user
	return s.reply, s.err
}

func TestParseDigestLines(t *testing.T) {
	got := parseDigestLines("```\nTITLE: Sailing Upwind Technique\nABSTRACT: How to tack. Reefing in a squall.\nKEYWORDS: mainsail, jib, reef\n```")
	if got.Title != "Sailing Upwind Technique" {
		t.Fatalf("title: %q", got.Title)
	}
	if !strings.HasPrefix(got.Abstract, "How to tack") {
		t.Fatalf("abstract: %q", got.Abstract)
	}
	if len(got.Keywords) != 3 || got.Keywords[0] != "mainsail" {
		t.Fatalf("keywords: %v", got.Keywords)
	}

	// Drift: lowercase keys, prose around it, "Summary" alias.
	got = parseDigestLines("Here is the label:\ntitle: Cold Proof Sourdough\nSummary: Long ferment opens the crumb.\nTags: dough, crumb, crust")
	if got.Title != "Cold Proof Sourdough" || got.Abstract == "" || len(got.Keywords) != 3 {
		t.Fatalf("drift parse failed: %+v", got)
	}
}

func TestLLMSummariser_HappyPath(t *testing.T) {
	c := &stubCompleter{reply: "TITLE: Region One\nABSTRACT: A thing.\nKEYWORDS: a, b, c"}
	s := &LLMSummariser{Complete: c, ModelName: "qwen"}
	d, err := s.Summarise(context.Background(), SummariseInput{Texts: []string{"hello world"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Region One" || d.Source != "llm:qwen" {
		t.Fatalf("digest: %+v", d)
	}
	if !strings.Contains(c.gotU, "hello world") {
		t.Fatalf("prompt missing chunk text: %q", c.gotU)
	}
}

func TestLLMSummariser_FallsBackPerField(t *testing.T) {
	// Model returns only a title; abstract + keywords come from extractive.
	c := &stubCompleter{reply: "TITLE: Partial Only"}
	s := &LLMSummariser{Complete: c}
	d, err := s.Summarise(context.Background(), SummariseInput{Texts: []string{
		"The mainsail luffed as the skipper tacked the sloop through the wind.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Partial Only" {
		t.Fatalf("title should be the model's: %q", d.Title)
	}
	if d.Abstract == "" || len(d.Keywords) == 0 {
		t.Fatalf("fallback did not fill missing fields: %+v", d)
	}
	if !strings.Contains(d.Source, "extractive") {
		t.Fatalf("source should note the mix: %q", d.Source)
	}
}

func TestLLMSummariser_FallsBackWhenCompleterDown(t *testing.T) {
	c := &stubCompleter{err: errors.New("model down")}
	s := &LLMSummariser{Complete: c}
	d, err := s.Summarise(context.Background(), SummariseInput{Texts: []string{
		"The mainsail luffed as the skipper tacked the sloop through the wind.",
	}})
	if err != nil {
		t.Fatalf("should degrade, not fail: %v", err)
	}
	if d.Source != "extractive" || d.Title == "" {
		t.Fatalf("expected a clean extractive digest: %+v", d)
	}
}

func TestLLMSummariser_PropagatesCtxCancel(t *testing.T) {
	c := &stubCompleter{err: errors.New("boom")}
	s := &LLMSummariser{Complete: c}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Summarise(ctx, SummariseInput{Texts: []string{"x"}}); err == nil {
		t.Fatal("cancelled ctx should propagate")
	}
}

func TestLLMSummariser_CapsChunks(t *testing.T) {
	c := &stubCompleter{reply: "TITLE: t\nABSTRACT: a\nKEYWORDS: k"}
	s := &LLMSummariser{Complete: c, MaxChunks: 2}
	texts := []string{"one", "two", "three", "four"}
	if _, err := s.Summarise(context.Background(), SummariseInput{Texts: texts}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.gotU, "three") {
		t.Fatalf("chunk cap not applied: %q", c.gotU)
	}
}
