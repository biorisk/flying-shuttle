package atlas

import (
	"context"
	"fmt"
	"strings"
)

// Completer is the shared instruct-LLM interface (same shape as
// stitch.Completer and search.Completer). One model process backs all three.
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

const digestSystemPrompt = `You label a cluster of transcript passages for a research index.
Reply with EXACTLY three lines and nothing else:
TITLE: a specific noun phrase, at most 6 words
ABSTRACT: 1 to 3 sentences on what this cluster is about
KEYWORDS: 3 to 8 comma-separated terms
Do not add commentary, markdown, or blank lines.`

// LLMSummariser digests a region with the shared instruct model. Any field the
// model omits or mangles falls back to the extractive summariser, so a flaky
// model degrades rather than fails the build.
type LLMSummariser struct {
	Complete Completer
	// Fallback supplies fields the model doesn't return. If nil, an
	// ExtractiveSummariser with no corpus keyworder is used.
	Fallback Summariser
	// MaxChunks caps how many member chunks go into the prompt. Default 15.
	MaxChunks int
	// ModelName is recorded in Digest.Source ("llm:<name>"). Default "llm".
	ModelName string
}

func (s *LLMSummariser) Summarise(ctx context.Context, in SummariseInput) (Digest, error) {
	max := s.MaxChunks
	if max <= 0 {
		max = 15
	}
	texts := in.Texts
	if len(texts) > max {
		texts = texts[:max]
	}

	var b strings.Builder
	for i, t := range texts {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, strings.TrimSpace(t))
	}

	fb := s.Fallback
	if fb == nil {
		fb = &ExtractiveSummariser{}
	}

	raw, err := s.Complete.Complete(ctx, digestSystemPrompt, b.String())
	if err != nil {
		// LLM unavailable (loading, crashed, OOM) — degrade to extractive for
		// this region rather than failing the whole build. ctx cancellation is
		// the exception: propagate it so a shutdown stops the build.
		if ctx.Err() != nil {
			return Digest{}, ctx.Err()
		}
		return fb.Summarise(ctx, in)
	}

	d := parseDigestLines(raw)
	d.Source = "llm:" + orString(s.ModelName, "llm")

	if d.Title == "" || d.Abstract == "" || len(d.Keywords) == 0 {
		if ex, ferr := fb.Summarise(ctx, in); ferr == nil {
			if d.Title == "" {
				d.Title = ex.Title
			}
			if d.Abstract == "" {
				d.Abstract = ex.Abstract
			}
			if len(d.Keywords) == 0 {
				d.Keywords = ex.Keywords
			}
			d.Source += "+extractive"
		}
	}
	return d, nil
}

// parseDigestLines pulls TITLE:/ABSTRACT:/KEYWORDS: out of a model reply,
// tolerating code fences, extra prose, and case/spacing drift.
func parseDigestLines(raw string) Digest {
	raw = stripThinking(raw)
	var d Digest
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "`*#> "))
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "TITLE":
			if d.Title == "" {
				d.Title = trimWords(val, 8)
			}
		case "ABSTRACT", "SUMMARY":
			if d.Abstract == "" {
				d.Abstract = val
			}
		case "KEYWORDS", "TAGS":
			if len(d.Keywords) == 0 {
				for _, k := range strings.Split(val, ",") {
					if k = strings.TrimSpace(strings.ToLower(k)); k != "" {
						d.Keywords = append(d.Keywords, k)
					}
				}
			}
		}
	}
	return d
}

// stripThinking drops a leading reasoning block emitted by "thinking" models
// (…</think>, <|channel>thought…<|message>, etc.) so we parse only the answer.
func stripThinking(raw string) string {
	for _, marker := range []string{"</think>", "<|message|>", "<|message>", "<|end_of_thought|>"} {
		if i := strings.LastIndex(raw, marker); i >= 0 {
			raw = raw[i+len(marker):]
		}
	}
	return raw
}

func trimWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

func orString(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
