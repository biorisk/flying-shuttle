package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// Guards that templ's style-attribute sanitizer keeps our rgba() shade.
func TestShadedSentenceStyleSurvivesSanitizer(t *testing.T) {
	vm := viewmodel.EvidencePane{
		Query: "budget", NodeID: "n1",
		Candidates: []viewmodel.Candidate{{
			ChunkID: "c1", SourceFile: "f.txt", Snippet: "…budget…", Match: "semantic", ScoreNorm: 0.72,
			Segments: []viewmodel.SnippetSeg{{Text: "budget", Mark: true}},
			FullSentences: []viewmodel.ShadedSentence{
				{Segments: []viewmodel.SnippetSeg{{Text: "The budget mattered.", Mark: false}}, Score: 1.0},
				{Segments: []viewmodel.SnippetSeg{{Text: "Nothing else did.", Mark: false}}, Score: 0.0},
			},
		}},
	}
	var b bytes.Buffer
	if err := components.Evidence(vm).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "rgba(100,108,255,0.38)") {
		t.Errorf("shade style dropped by sanitizer:\n%s", out)
	}
	if !strings.Contains(out, "cand-sent") {
		t.Errorf("shaded sentence not rendered")
	}
	if !strings.Contains(out, "badge-semantic") || !strings.Contains(out, ">semantic<") {
		t.Errorf("match badge not rendered:\n%s", out)
	}
	if !strings.Contains(out, "width:72%") {
		t.Errorf("score bar width dropped by sanitizer:\n%s", out)
	}
}
