package atlas

import (
	"context"
	"strings"
	"unicode"
)

// SummariseInput carries what a Summariser needs to digest one region.
type SummariseInput struct {
	// Texts are the region's member chunk contents, ordered by ascending
	// distance to the centroid — Texts[0] is the most representative chunk.
	Texts []string
}

// Summariser turns a region's member chunks into a Digest. The extractive
// implementation needs no model; the LLM implementation (a separate task)
// swaps in once the shared instruct model is chosen.
type Summariser interface {
	Summarise(ctx context.Context, in SummariseInput) (Digest, error)
}

// ExtractiveSummariser produces a Digest with no LLM: keywords and title from
// TF-IDF, abstract from the opening sentences of the centroid-nearest chunk.
type ExtractiveSummariser struct {
	KW *Keyworder

	// TitleTerms / KeywordCount / AbstractSentences override the defaults
	// (3 / 6 / 2) when non-zero.
	TitleTerms        int
	KeywordCount      int
	AbstractSentences int
}

func (e *ExtractiveSummariser) Summarise(_ context.Context, in SummariseInput) (Digest, error) {
	titleTerms := orDefault(e.TitleTerms, 3)
	kwCount := orDefault(e.KeywordCount, 6)
	sentences := orDefault(e.AbstractSentences, 2)

	kw := e.KW
	if kw == nil {
		// No corpus keyworder supplied — fall back to IDF over just this
		// region's texts. Coarser, but keeps the extractive path working.
		kw = NewKeyworder(in.Texts)
	}
	keywords := kw.TopFromDocs(in.Texts, kwCount)

	titleParts := keywords
	if len(titleParts) > titleTerms {
		titleParts = titleParts[:titleTerms]
	}

	var abstract string
	if len(in.Texts) > 0 {
		abstract = firstSentences(in.Texts[0], sentences)
	}

	return Digest{
		Title:    titleCase(strings.Join(titleParts, " · ")),
		Abstract: abstract,
		Keywords: keywords,
		Source:   "extractive",
	}, nil
}

func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// titleCase upper-cases the first letter of each space-separated word.
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	words := strings.Split(s, " ")
	for i, w := range words {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// firstSentences returns up to n leading sentences of text, whitespace-
// collapsed and trimmed.
func firstSentences(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	var sentences []string
	start := 0
	for i, r := range runes {
		if r == '.' || r == '!' || r == '?' {
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
				sentences = append(sentences, strings.TrimSpace(string(runes[start:i+1])))
				start = i + 1
				if len(sentences) >= n {
					break
				}
			}
		}
	}
	out := strings.Join(sentences, " ")
	if out == "" {
		// No sentence punctuation — fall back to a leading slice.
		if len(runes) > 280 {
			return strings.TrimSpace(string(runes[:280])) + "…"
		}
		return text
	}
	return out
}
