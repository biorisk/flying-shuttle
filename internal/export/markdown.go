// Package export converts linearized DAG output into exportable formats.
package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/stitch"
)

// ExportFormat specifies the output format.
type ExportFormat string

const (
	FormatMarkdown ExportFormat = "markdown"
)

// ExportRequest configures an export operation.
type ExportRequest struct {
	ThreadID  string // empty = full manuscript
	GlueLevel int
	Title     string // document title
}

// ExportResult is the exported document.
type ExportResult struct {
	Format  ExportFormat `json:"format"`
	Content string       `json:"content"`
}

// ToMarkdown converts a LinearizeResult into a Markdown document with anchors.
func ToMarkdown(result *dag.LinearizeResult, title string, edges []model.Edge) string {
	var sb strings.Builder

	// Document title.
	if title != "" {
		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
	}

	// Build a map of outgoing edges per node for CYOA exits.
	exitMap := make(map[string][]model.Edge)
	for _, e := range edges {
		if e.Type == model.EdgeTypeBranch || e.Type == model.EdgeTypeJump {
			exitMap[e.FromNode] = append(exitMap[e.FromNode], e)
		}
	}

	// Build a node title lookup.
	titleMap := make(map[string]string)
	for _, n := range result.Nodes {
		titleMap[n.ID] = n.Title
	}

	// Write each node as a section.
	for i, n := range result.Nodes {
		// Anchor for internal links.
		anchor := Slugify(n.ID)
		sb.WriteString(fmt.Sprintf("<a id=\"%s\"></a>\n\n", anchor))

		// Section heading.
		heading := n.Title
		if heading == "" {
			heading = fmt.Sprintf("Section %d", i+1)
		}
		sb.WriteString("## ")
		sb.WriteString(heading)
		sb.WriteString("\n\n")
	}

	// Write the stitched content with attribution markers.
	if result.Stitch != nil && len(result.Stitch.Spans) > 0 {
		sb.WriteString("---\n\n")
		for _, span := range result.Stitch.Spans {
			if span.Type == stitch.SpanGlue {
				// Mark glue text in italics.
				sb.WriteString("*")
				sb.WriteString(strings.TrimSpace(span.Text))
				sb.WriteString("* ")
			} else {
				sb.WriteString(span.Text)
			}
		}
		sb.WriteString("\n\n")
	}

	// Write CYOA exits as link lists at the end.
	hasExits := false
	for _, n := range result.Nodes {
		exits := exitMap[n.ID]
		if len(exits) == 0 {
			continue
		}
		if !hasExits {
			sb.WriteString("---\n\n### Navigation\n\n")
			hasExits = true
		}
		sb.WriteString(fmt.Sprintf("**From %s:**\n\n", n.Title))
		for _, e := range exits {
			target := titleMap[e.ToNode]
			if target == "" {
				target = e.ToNode
			}
			anchor := Slugify(e.ToNode)
			label := target
			if e.Type == model.EdgeTypeBranch {
				label = "→ " + label
			}
			if e.Condition != nil && *e.Condition != "" {
				label += fmt.Sprintf(" *(if %s)*", *e.Condition)
			}
			sb.WriteString(fmt.Sprintf("- [%s](#%s)\n", label, anchor))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// slugify creates a URL-safe anchor from a string.
// Slugify creates a URL-safe anchor from a string.
func Slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, s)
	return s
}

// GenerateMarkdown is the high-level export function that linearizes and converts.
func GenerateMarkdown(s doc.Store, stitcher stitch.Stitcher, req ExportRequest) (*ExportResult, error) {
	mode := dag.ModeManuscript
	if req.ThreadID != "" {
		mode = dag.ModeThread
	}

	result, err := dag.LinearizeAndStitch(
		context.Background(),
		s, stitcher,
		dag.LinearizeRequest{
			Mode:      mode,
			ThreadID:  req.ThreadID,
			GlueLevel: req.GlueLevel,
		},
	)
	if err != nil {
		return nil, err
	}

	edges, err := s.ListEdges()
	if err != nil {
		return nil, err
	}

	md := ToMarkdown(result, req.Title, edges)
	return &ExportResult{
		Format:  FormatMarkdown,
		Content: md,
	}, nil
}
