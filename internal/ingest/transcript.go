package ingest

import (
	"regexp"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
)

// TextExtensions are the non-audio upload formats treated as ready-made
// transcripts. Their contents are ingested directly, skipping transcription.
var TextExtensions = map[string]bool{
	".txt":      true,
	".md":       true,
	".markdown": true,
	".text":     true,
}

// IsTextTranscript reports whether ext (with leading dot, any case) names a
// transcript text format handled by ParseTranscript.
func IsTextTranscript(ext string) bool {
	return TextExtensions[strings.ToLower(ext)]
}

// targetChunkWords is the approximate size of a transcript chunk. A chunk may
// run over when a single long paragraph pushes past the target.
const targetChunkWords = 160

var (
	paragraphSplit = regexp.MustCompile(`\n[ \t]*\n+`)
	whitespaceRun  = regexp.MustCompile(`[ \t\r\n]+`)
)

// ParseTranscript splits raw plain-text transcript content into ordered
// segments — one segment per sentence, grouped from blank-line-separated
// paragraphs — so downstream chunking has sensible granularity.
func ParseTranscript(raw string) []model.TranscriptSegment {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")

	var segs []model.TranscriptSegment
	for _, para := range paragraphSplit.Split(raw, -1) {
		if strings.TrimSpace(para) == "" {
			continue
		}
		for _, sent := range splitSentences(para) {
			sent = strings.TrimSpace(whitespaceRun.ReplaceAllString(sent, " "))
			if sent == "" {
				continue
			}
			segs = append(segs, model.TranscriptSegment{Text: sent})
		}
	}
	return segs
}

// splitSentences breaks text on sentence-ending punctuation followed by
// whitespace. Text with no such boundary is returned as a single element.
func splitSentences(text string) []string {
	var out []string
	var b strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		b.WriteRune(r)
		if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) {
			switch runes[i+1] {
			case ' ', '\n', '\t':
				out = append(out, b.String())
				b.Reset()
			}
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

// ChunkTranscript groups ordered transcript segments into immutable Chunk
// records of roughly targetChunkWords words each. Chunks carry no embedding
// vector — real vectors come from the offline .fembed pipeline, keyed on
// SourceFile + Content.
func ChunkTranscript(sourceFile string, segments []model.TranscriptSegment) []model.Chunk {
	var chunks []model.Chunk
	var buf []string
	words := 0
	pos := 0

	flush := func() {
		if len(buf) == 0 {
			return
		}
		content := strings.Join(buf, " ")
		chunks = append(chunks, model.Chunk{
			ID:          uuid.NewString(),
			SourceFile:  sourceFile,
			Content:     content,
			StartOffset: pos,
			EndOffset:   pos + len(content),
		})
		pos += len(content) + 1
		buf = nil
		words = 0
	}

	for i := range segments {
		buf = append(buf, segments[i].Text)
		words += len(strings.Fields(segments[i].Text))
		if words >= targetChunkWords {
			flush()
		}
	}
	flush()
	return chunks
}
