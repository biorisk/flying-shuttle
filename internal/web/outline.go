package web

import (
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// outlineView renders the outline with no thread scope or diff.
func (h *handlers) outlineView() (viewmodel.Outline, error) {
	return h.outlineViewOpts(outlineOpts{})
}

func (h *handlers) outlineViewFor(threadID string) (viewmodel.Outline, error) {
	return h.outlineViewOpts(outlineOpts{ThreadID: threadID})
}

type outlineOpts struct {
	ThreadID    string
	DiffAgainst string // snapshot or branch id
}

// outlineViewOpts builds the outline render model: flattened prev/next
// neighbours, optional thread membership, and optional diff decorations
// (added/changed classes + ghost rows for removed bullets).
func (h *handlers) outlineViewOpts(opts outlineOpts) (viewmodel.Outline, error) {
	forest, err := h.d.Outline.Tree()
	if err != nil {
		return viewmodel.Outline{}, err
	}

	inThread := map[string]bool{}
	if opts.ThreadID != "" {
		if tns, err := h.d.Store.GetThreadNodes(opts.ThreadID); err == nil {
			for _, tn := range tns {
				inThread[tn.NodeID] = true
			}
		}
	}

	var diff outline.DiffResult
	if opts.DiffAgainst != "" {
		diff = h.computeOutlineDiff(opts.DiffAgainst)
	}
	// group ghosts by anchor parent
	ghostsUnder := map[string][]outline.Ghost{}
	for _, g := range diff.Ghosts {
		ghostsUnder[g.ParentID] = append(ghostsUnder[g.ParentID], g)
	}

	flat := outline.Flatten(forest)
	prev := make(map[string]string, len(flat))
	next := make(map[string]string, len(flat))
	for i, tn := range flat {
		if i > 0 {
			prev[tn.Node.ID] = flat[i-1].Node.ID
		}
		if i+1 < len(flat) {
			next[tn.Node.ID] = flat[i+1].Node.ID
		}
	}

	ghostNode := func(g outline.Ghost, depth int) viewmodel.OutlineNode {
		return viewmodel.OutlineNode{
			ID: g.Node.ID, Title: g.Node.Title, Body: g.Node.Body,
			Type: "outline", Depth: depth, Ghost: true,
		}
	}

	// Which evidence bullets carry an author-edited excerpt (§5.6).
	editedNode := map[string]bool{}
	if evs, err := h.d.Store.ListAllEvidence(); err == nil {
		for _, e := range evs {
			if e.Edited {
				editedNode[e.NodeID] = true
			}
		}
	}

	var conv func(tn *outline.TreeNode) viewmodel.OutlineNode
	conv = func(tn *outline.TreeNode) viewmodel.OutlineNode {
		n := viewmodel.OutlineNode{
			ID:       tn.Node.ID,
			Title:    tn.Node.Title,
			Body:     tn.Node.Body,
			Type:     string(tn.Node.Type),
			Version:  tn.Node.Version,
			Locked:   tn.Node.Locked,
			Evidence: tn.Node.Type == model.NodeTypeChunkRef,
			Edited:   editedNode[tn.Node.ID],
			Depth:    tn.Depth,
			Prev:     prev[tn.Node.ID],
			Next:     next[tn.Node.ID],
			InThread: inThread[tn.Node.ID],
			Diff:     string(diff.Status[tn.Node.ID]),
		}
		for _, c := range tn.Children {
			n.Children = append(n.Children, conv(c))
		}
		for _, g := range ghostsUnder[tn.Node.ID] {
			n.Children = append(n.Children, ghostNode(g, tn.Depth+1))
		}
		return n
	}

	vm := viewmodel.Outline{ThreadID: opts.ThreadID, DiffAgainst: opts.DiffAgainst}
	for _, r := range forest {
		vm.Roots = append(vm.Roots, conv(r))
	}
	for _, g := range ghostsUnder[""] {
		vm.Roots = append(vm.Roots, ghostNode(g, 0))
	}
	return vm, nil
}

// computeOutlineDiff loads the baseline (snapshot first, then branch) and
// diffs the current outline against it.
func (h *handlers) computeOutlineDiff(id string) outline.DiffResult {
	var baseNodes []model.Node
	var baseEdges []model.Edge

	if snap, err := h.d.Store.GetSnapshot(id); err == nil {
		baseNodes, baseEdges = snap.Data.Nodes, snap.Data.Edges
	} else if br, err := h.d.Store.GetBranch(id); err == nil {
		baseNodes, baseEdges = br.Data.Nodes, br.Data.Edges
	} else {
		return outline.DiffResult{Status: map[string]outline.DiffStatus{}}
	}

	curNodes, _ := h.d.Store.ListNodes()
	curEdges, _ := h.d.Store.ListEdges()
	return outline.Diff(curNodes, curEdges, baseNodes, baseEdges)
}
