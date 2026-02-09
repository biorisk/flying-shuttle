package stitch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PromptStitcher generates a structured LLM prompt and parses the response.
// It delegates actual LLM calls to a Completer interface so it can be tested
// with deterministic mocks.
type PromptStitcher struct {
	Complete Completer
}

// Completer sends a prompt to an LLM and returns the raw text response.
type Completer interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// systemPrompt is the core instruction set for constrained stitching.
const systemPrompt = `You are a minimal-intervention text stitcher. Your ONLY job is to make raw transcript chunks read as a single coherent passage.

Rules:
1. Output every chunk in the EXACT order given — do not reorder.
2. Add the ABSOLUTE MINIMUM words needed for coherence (transitions, pronouns, conjunctions).
3. DO NOT rewrite, paraphrase, summarize, or expand any chunk text.
4. DO NOT correct grammar, spelling, or phrasing in the original chunks.
5. Mark every piece of your output as either a verbatim chunk or glue you added.

Output a JSON array of objects. Each object is one of:
  {"type":"chunk","index":<0-based>,"text":"<exact original chunk text>"}
  {"type":"glue","text":"<your added transition text>"}

Return ONLY the JSON array — no markdown fences, no commentary.`

// glueInstructions maps GlueLevel ranges to additional instructions.
func glueInstruction(level int) string {
	switch {
	case level <= 0:
		return "Add NO transition text at all. Output only chunk spans."
	case level <= 25:
		return "Add only single conjunctions (and, but, then) between chunks when absolutely necessary."
	case level <= 50:
		return "Add brief transitional phrases (1-5 words) between chunks for readability."
	case level <= 75:
		return "Add transitional sentences where needed for flow, but keep them short."
	default:
		return "Add transitions freely to make the text read as polished prose, but still preserve all original chunk text verbatim."
	}
}

func (ps *PromptStitcher) Stitch(ctx context.Context, req Request) (*Result, error) {
	if len(req.Chunks) == 0 {
		return buildResult(nil), nil
	}

	// Single chunk: no stitching needed.
	if len(req.Chunks) == 1 {
		return buildResult([]Span{{
			Type:       SpanChunk,
			ChunkIndex: 0,
			ChunkID:    req.Chunks[0].ID,
			Text:       req.Chunks[0].Content,
		}}), nil
	}

	glue := req.GlueLevel
	if glue == 0 {
		glue = 50
	}

	// Build user prompt with numbered chunks.
	var b strings.Builder
	fmt.Fprintf(&b, "Glue level instruction: %s\n\n", glueInstruction(glue))
	b.WriteString("Chunks to stitch:\n\n")
	for i, c := range req.Chunks {
		speaker := ""
		if c.Speaker != "" {
			speaker = fmt.Sprintf(" [Speaker: %s]", c.Speaker)
		}
		fmt.Fprintf(&b, "--- Chunk %d%s ---\n%s\n\n", i, speaker, c.Content)
	}

	raw, err := ps.Complete.Complete(ctx, systemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("stitch LLM call: %w", err)
	}

	spans, err := parseSpans(raw, req.Chunks)
	if err != nil {
		return nil, fmt.Errorf("parse stitch response: %w", err)
	}

	return buildResult(spans), nil
}

// rawSpan is the JSON structure we expect from the LLM.
type rawSpan struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// parseSpans parses the LLM JSON response into attributed Spans.
func parseSpans(raw string, chunks []ChunkInput) ([]Span, error) {
	// Strip markdown code fences if the LLM wraps the output.
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		// Remove first and last lines (fences).
		if len(lines) >= 2 {
			lines = lines[1:]
			if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				lines = lines[:len(lines)-1]
			}
		}
		raw = strings.Join(lines, "\n")
	}

	var raws []rawSpan
	if err := json.Unmarshal([]byte(raw), &raws); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w\nraw: %s", err, raw)
	}

	spans := make([]Span, 0, len(raws))
	for _, rs := range raws {
		switch rs.Type {
		case "chunk":
			id := ""
			if rs.Index >= 0 && rs.Index < len(chunks) {
				id = chunks[rs.Index].ID
			}
			spans = append(spans, Span{
				Type:       SpanChunk,
				ChunkIndex: rs.Index,
				ChunkID:    id,
				Text:       rs.Text,
			})
		case "glue":
			spans = append(spans, Span{
				Type: SpanGlue,
				Text: rs.Text,
			})
		default:
			return nil, fmt.Errorf("unknown span type %q", rs.Type)
		}
	}

	return spans, nil
}
