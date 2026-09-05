package export

import (
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/doc"
)

func setupStore(t *testing.T) doc.Store {
	t.Helper()
	s, err := doc.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestToMarkdown_basic(t *testing.T) {
	result := &dag.LinearizeResult{
		Nodes: []model.Node{
			{ID: "n1", Title: "Introduction"},
			{ID: "n2", Title: "Conclusion"},
		},
		Stitch: &stitch.Result{
			Spans: []stitch.Span{
				{Type: stitch.SpanChunk, Text: "Once upon a time."},
				{Type: stitch.SpanGlue, Text: " Meanwhile, "},
				{Type: stitch.SpanChunk, Text: "The end."},
			},
			Text: "Once upon a time. Meanwhile, The end.",
		},
	}

	md := ToMarkdown(result, "My Story", nil)

	if !strings.Contains(md, "# My Story") {
		t.Fatal("expected document title")
	}
	if !strings.Contains(md, "## Introduction") {
		t.Fatal("expected section heading")
	}
	if !strings.Contains(md, "## Conclusion") {
		t.Fatal("expected section heading")
	}
	if !strings.Contains(md, "Once upon a time.") {
		t.Fatal("expected chunk text")
	}
	if !strings.Contains(md, "*Meanwhile,*") {
		t.Fatal("expected glue text in italics")
	}
}

func TestToMarkdown_anchors(t *testing.T) {
	result := &dag.LinearizeResult{
		Nodes: []model.Node{
			{ID: "n1", Title: "Chapter One"},
		},
		Stitch: &stitch.Result{},
	}

	md := ToMarkdown(result, "", nil)
	if !strings.Contains(md, `<a id="n1"></a>`) {
		t.Fatal("expected anchor tag for node")
	}
}

func TestToMarkdown_branchExits(t *testing.T) {
	result := &dag.LinearizeResult{
		Nodes: []model.Node{
			{ID: "n1", Title: "Fork"},
			{ID: "n2", Title: "Path A"},
			{ID: "n3", Title: "Path B"},
		},
		Stitch: &stitch.Result{},
	}
	cond := "audience==novice"
	edges := []model.Edge{
		{ID: "e1", FromNode: "n1", ToNode: "n2", Type: model.EdgeTypeBranch},
		{ID: "e2", FromNode: "n1", ToNode: "n3", Type: model.EdgeTypeBranch, Condition: &cond},
	}

	md := ToMarkdown(result, "CYOA", edges)
	if !strings.Contains(md, "[→ Path A]") {
		t.Fatal("expected branch exit link to Path A")
	}
	if !strings.Contains(md, "audience==novice") {
		t.Fatal("expected condition in exit link")
	}
}

func TestToMarkdown_emptyNodes(t *testing.T) {
	result := &dag.LinearizeResult{
		Nodes:  nil,
		Stitch: &stitch.Result{},
	}
	md := ToMarkdown(result, "Empty", nil)
	if !strings.Contains(md, "# Empty") {
		t.Fatal("expected title even with no nodes")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Hello World", "hello-world"},
		{"node-123", "node-123"},
		{"A B!C", "a-bc"},
		{"", ""},
	}
	for _, tt := range tests {
		got := Slugify(tt.in)
		if got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGenerateMarkdown_integration(t *testing.T) {
	s := setupStore(t)

	n1 := &model.Node{ID: "n1", Type: "outline", Title: "Intro", Body: "Hello world"}
	n2 := &model.Node{ID: "n2", Type: "outline", Title: "End", Body: "Goodbye"}
	if err := s.CreateNode(n1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateNode(n2); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEdge(&model.Edge{ID: "e1", FromNode: "n1", ToNode: "n2", Type: "linear"}); err != nil {
		t.Fatal(err)
	}

	stitcher := &stitch.StubStitcher{}
	result, err := GenerateMarkdown(s, stitcher, ExportRequest{Title: "Test Doc"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatMarkdown {
		t.Fatalf("expected markdown format, got %s", result.Format)
	}
	if !strings.Contains(result.Content, "# Test Doc") {
		t.Fatal("expected title in output")
	}
	if !strings.Contains(result.Content, "Hello world") {
		t.Fatal("expected body content")
	}
}
