package atlas

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ChunkLabeller writes a short label for each chunk with the shared instruct
// model, in batches. It's used only for the transcript drill-down view's node
// labels; labels are persisted per chunk (atlas_chunk_label). A real
// "llm:<model>" label is written once and never recomputed; a "head"
// fallback (LLM down / mangled line) is re-attempted on the next build.
type ChunkLabeller struct {
	Complete  Completer
	ModelName string // recorded in ChunkLabel.Source ("llm:<name>"); default "llm"
	// Batch is how many chunks go into one prompt. Default 12.
	Batch int
	// MaxWords caps a returned label. Default 6.
	MaxWords int
}

// LabelInput is one chunk to label.
type LabelInput struct {
	ChunkID string
	Text    string
}

const chunkLabelSystemPrompt = `You label passages from a transcript for a navigation index.
You are given numbered passages. For EACH passage, output exactly one line:
<number>. <a specific 3 to 6 word noun phrase naming what that passage is about>
Rules: one line per passage, in order; no commentary, no markdown, no blank lines;
the phrase names the passage's actual topic, not the speaker or the format.`

var labelLineRE = regexp.MustCompile(`^\s*\[?(\d+)[.):\]]\s*(.+?)\s*$`)

// Label labels every input in batches, calling persist once per batch so an
// interrupted build keeps its progress (the next build only re-labels what's
// still missing). On a successful model call, passages the model labelled get
// source "llm:<model>"; any it skipped or mangled fall back to a text head
// with source "head". If a model call fails (LLM down), Label returns the
// error without persisting that batch, so the next build retries it.
func (l *ChunkLabeller) Label(ctx context.Context, in []LabelInput, persist func([]ChunkLabel) error) error {
	batch := l.Batch
	if batch <= 0 {
		batch = 12
	}
	maxWords := l.MaxWords
	if maxWords <= 0 {
		maxWords = 6
	}
	model := l.ModelName
	if model == "" {
		model = "llm"
	}

	for start := 0; start < len(in); start += batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + batch
		if end > len(in) {
			end = len(in)
		}
		group := in[start:end]

		var b strings.Builder
		for i, c := range group {
			fmt.Fprintf(&b, "%d. %s\n\n", i+1, strings.Join(strings.Fields(c.Text), " "))
		}

		raw, err := l.Complete.Complete(ctx, chunkLabelSystemPrompt, b.String())
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("chunk label batch %d-%d: %w", start, end, err)
		}

		parsed := parseChunkLabels(raw, len(group), maxWords)
		out := make([]ChunkLabel, len(group))
		for i, c := range group {
			if lbl := parsed[i]; lbl != "" {
				out[i] = ChunkLabel{ChunkID: c.ChunkID, Label: lbl, Source: "llm:" + model}
			} else {
				out[i] = ChunkLabel{ChunkID: c.ChunkID, Label: headLabel(c.Text, maxWords), Source: "head"}
			}
		}
		if err := persist(out); err != nil {
			return err
		}
	}
	return nil
}

// parseChunkLabels pulls "<n>. <phrase>" lines out of a model reply into a
// slice of length n (missing entries left "").
func parseChunkLabels(raw string, n, maxWords int) []string {
	raw = stripThinking(raw)
	out := make([]string, n)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "`*#>- "))
		m := labelLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		idx := 0
		for _, r := range m[1] {
			idx = idx*10 + int(r-'0')
		}
		idx-- // 1-based -> 0-based
		if idx < 0 || idx >= n || out[idx] != "" {
			continue
		}
		phrase := strings.Trim(m[2], `"'.`)
		out[idx] = trimWords(phrase, maxWords)
	}
	return out
}

// headLabel is the fallback label: the first few words of the chunk.
func headLabel(text string, maxWords int) string {
	f := strings.Fields(text)
	if len(f) > maxWords {
		f = f[:maxWords]
	}
	return strings.Join(f, " ")
}
