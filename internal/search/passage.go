package search

import (
	"strconv"
	"strings"
)

// Dual-granularity indexing: alongside the ~160-word "reading" chunks, the
// HybridIndex keeps a second BM25 index over small overlapping passages carved
// out of each chunk. Passages are the retrieval unit — matching a 55-word
// window ranks the chunk and, because the passage's own offsets are known, the
// evidence pane opens roughly on the relevant text by construction. The reader
// still stitches the surrounding chunk back for context.
//
// Passages are not snapshotted; they are rebuilt from the store at startup.

const (
	passageWords       = 55
	passageStrideWords = 40 // 15-word overlap between adjacent passages
	passageIDSep       = "\x00"
)

// PassageID encodes a chunk id together with the passage's [start,end) rune
// offsets within that chunk.
func PassageID(chunkID string, s Span) string {
	return chunkID + passageIDSep + strconv.Itoa(s.Start) + passageIDSep + strconv.Itoa(s.End)
}

// SplitPassageID reverses PassageID.
func SplitPassageID(id string) (chunkID string, span Span, ok bool) {
	parts := strings.SplitN(id, passageIDSep, 3)
	if len(parts) != 3 {
		return "", Span{}, false
	}
	start, err1 := strconv.Atoi(parts[1])
	end, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || end <= start {
		return "", Span{}, false
	}
	return parts[0], Span{start, end}, true
}

// Passage is one retrieval unit: a rune span of a chunk plus its text.
type Passage struct {
	Span
	Text string
}

// chunkPassages splits content into overlapping word windows.
func chunkPassages(content string) []Passage {
	toks := tokenizeWithPositions(content)
	if len(toks) == 0 {
		return nil
	}
	runes := []rune(content)
	var out []Passage
	for i := 0; i < len(toks); i += passageStrideWords {
		j := i + passageWords
		if j > len(toks) {
			j = len(toks)
		}
		sp := Span{toks[i].Start, toks[j-1].End}
		out = append(out, Passage{Span: sp, Text: string(runes[sp.Start:sp.End])})
		if j == len(toks) {
			break
		}
	}
	return out
}
