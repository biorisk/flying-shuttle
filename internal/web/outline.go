package web

import (
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// outlineView reads the current outline tree into a render model, computing
// flattened-order prev/next neighbours for keyboard navigation.
func (h *handlers) outlineView() (viewmodel.Outline, error) {
	forest, err := h.d.Outline.Tree()
	if err != nil {
		return viewmodel.Outline{}, err
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
			Depth:    tn.Depth,
			Prev:     prev[tn.Node.ID],
			Next:     next[tn.Node.ID],
		}
		for _, c := range tn.Children {
			n.Children = append(n.Children, conv(c))
		}
		return n
	}

	vm := viewmodel.Outline{}
	for _, r := range forest {
		vm.Roots = append(vm.Roots, conv(r))
	}
	return vm, nil
}
