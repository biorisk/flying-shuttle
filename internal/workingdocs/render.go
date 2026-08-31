// Package workingdocs mirrors a project's DAG state to two files that live
// next to the SQLite database: outline.md (human-readable) and state.json
// (lossless, for recovery). Both are fully regenerated and written atomically
// whenever the state changes, so work survives a lost or corrupt database.
package workingdocs

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
)

// RenderOutline turns a project's DAG state into a Markdown document: the
// outline as nested bullets, evidence as blockquotes with a source citation,
// non-linear exits as "→" lines, then the threads and branches.
func RenderOutline(project string, data *model.SnapshotData, branches []model.BranchSummary) string {
	var b strings.Builder

	title := project
	if title == "" {
		title = "outline"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)

	bullets, withEvidence := outlineStats(data)
	fmt.Fprintf(&b, "_updated %s · %d bullet(s), %d with evidence_\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 MST"), bullets, withEvidence)

	forest := outline.BuildTree(data.Nodes, data.Edges)
	if len(forest) == 0 {
		b.WriteString("_(empty outline)_\n")
	}

	exits := exitsByNode(data)
	for _, root := range forest {
		writeNode(&b, root, exits, 0)
	}

	writeThreads(&b, data, forest)
	writeBranches(&b, branches)

	return b.String()
}

func writeNode(b *strings.Builder, tn *outline.TreeNode, exits map[string][]string, depth int) {
	indent := strings.Repeat("  ", depth)

	if tn.Node.Type == model.NodeTypeChunkRef {
		src := tn.Node.Labels["source_file"]
		text := collapseWS(tn.Node.Body)
		if text == "" {
			text = collapseWS(tn.Node.Title)
		}
		if src != "" {
			fmt.Fprintf(b, "%s> %s — _%s_\n", indent, text, src)
		} else {
			fmt.Fprintf(b, "%s> %s\n", indent, text)
		}
		return
	}

	title := strings.TrimSpace(tn.Node.Title)
	if title == "" {
		title = "_(untitled)_"
	}
	lock := ""
	if tn.Node.Locked {
		lock = "  `[locked]`"
	}
	fmt.Fprintf(b, "%s- %s%s\n", indent, title, lock)

	if body := strings.TrimSpace(tn.Node.Body); body != "" {
		for _, line := range strings.Split(body, "\n") {
			fmt.Fprintf(b, "%s  %s\n", indent, line)
		}
	}
	for _, ex := range exits[tn.Node.ID] {
		fmt.Fprintf(b, "%s  → %s\n", indent, ex)
	}
	for _, c := range tn.Children {
		writeNode(b, c, exits, depth+1)
	}
}

func writeThreads(b *strings.Builder, data *model.SnapshotData, forest []*outline.TreeNode) {
	if len(data.Threads) == 0 {
		return
	}
	title := map[string]string{}
	for _, n := range data.Nodes {
		title[n.ID] = n.Title
	}
	byThread := map[string][]model.ThreadNode{}
	for _, tn := range data.ThreadNodes {
		byThread[tn.ThreadID] = append(byThread[tn.ThreadID], tn)
	}
	total := len(outline.Flatten(forest))

	b.WriteString("\n## Threads\n")
	threads := append([]model.Thread(nil), data.Threads...)
	sort.Slice(threads, func(i, j int) bool { return threads[i].Name < threads[j].Name })
	for _, t := range threads {
		tns := byThread[t.ID]
		sort.Slice(tns, func(i, j int) bool { return tns[i].Position < tns[j].Position })
		fmt.Fprintf(b, "\n### %s  (%d of %d bullets)\n", orName(t.Name), len(tns), total)
		for i, tn := range tns {
			fmt.Fprintf(b, "%d. %s\n", i+1, orUntitled(title[tn.NodeID]))
		}
	}
}

func writeBranches(b *strings.Builder, branches []model.BranchSummary) {
	if len(branches) == 0 {
		return
	}
	b.WriteString("\n## Branches\n\n")
	branches = append([]model.BranchSummary(nil), branches...)
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	for _, br := range branches {
		active := ""
		if br.Active {
			active = " _(active)_"
		}
		fmt.Fprintf(b, "- **%s**%s\n", br.Name, active)
	}
}

// --- helpers ---

// outlineStats returns the number of outline bullets and how many of them have
// at least one evidence (chunk_ref) child.
func outlineStats(data *model.SnapshotData) (bullets, withEvidence int) {
	typ := map[string]model.NodeType{}
	for _, n := range data.Nodes {
		typ[n.ID] = n.Type
		if n.Type == model.NodeTypeOutline {
			bullets++
		}
	}
	hasEv := map[string]bool{}
	for _, e := range data.Edges {
		if e.Type == model.EdgeTypeLinear && typ[e.ToNode] == model.NodeTypeChunkRef {
			hasEv[e.FromNode] = true
		}
	}
	return bullets, len(hasEv)
}

func exitsByNode(data *model.SnapshotData) map[string][]string {
	title := map[string]string{}
	for _, n := range data.Nodes {
		title[n.ID] = n.Title
	}
	out := map[string][]string{}
	for _, e := range data.Edges {
		if e.Type == model.EdgeTypeLinear {
			continue
		}
		label := fmt.Sprintf("%s: %s", e.Type, orUntitled(title[e.ToNode]))
		if e.Condition != nil && *e.Condition != "" {
			label += fmt.Sprintf("  (if %s)", *e.Condition)
		}
		out[e.FromNode] = append(out[e.FromNode], label)
	}
	return out
}

func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

func orName(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(unnamed thread)"
	}
	return s
}

func orUntitled(s string) string {
	if strings.TrimSpace(s) == "" {
		return "_(untitled)_"
	}
	return s
}
