// Package outline builds and manipulates the nested bullet tree that the
// editor renders. The tree is derived from flat nodes + linear edges: it is
// the Go port of the React `outlineStore.buildTree` logic, and the single
// source of truth for outline structure now that rendering is server-side.
package outline

import (
	"sort"

	"github.com/biorisk/flying-shuttle/internal/model"
)

// TreeNode is one bullet in the outline tree.
type TreeNode struct {
	Node     model.Node  `json:"node"`
	Children []*TreeNode `json:"children"`
	ParentID string      `json:"parent_id"` // "" for roots
	Depth    int         `json:"depth"`
}

// BuildTree assembles the outline forest from flat nodes and edges.
//
// Semantics:
//   - outline nodes and their chunk_ref (evidence) children participate;
//     "synth" nodes and orphan chunk_refs are ignored
//   - only edges of type "linear" define parent → child
//   - a root is an *outline* node with no incoming linear edge (a chunk_ref is
//     never a root)
//   - siblings are ordered by edge weight, ties broken by node creation time
//   - roots are ordered by node creation time
//
// This matches the former client `buildTree` for outline structure, and
// additionally surfaces the locked evidence sub-bullets that attach-evidence
// creates as chunk_ref children.
func BuildTree(nodes []model.Node, edges []model.Edge) []*TreeNode {
	nodeByID := make(map[string]model.Node, len(nodes))
	var order []string // outline node IDs in creation order (root candidates)
	for _, n := range nodes {
		switch n.Type {
		case model.NodeTypeOutline:
			nodeByID[n.ID] = n
			order = append(order, n.ID)
		case model.NodeTypeChunkRef:
			nodeByID[n.ID] = n // eligible as a child, never a root
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := nodeByID[order[i]], nodeByID[order[j]]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})

	type childRef struct {
		id     string
		weight int
	}
	childrenOf := make(map[string][]childRef)
	hasParent := make(map[string]bool)
	for _, e := range edges {
		if e.Type != model.EdgeTypeLinear {
			continue
		}
		if _, ok := nodeByID[e.FromNode]; !ok {
			continue
		}
		if _, ok := nodeByID[e.ToNode]; !ok {
			continue
		}
		childrenOf[e.FromNode] = append(childrenOf[e.FromNode], childRef{e.ToNode, e.Weight})
		hasParent[e.ToNode] = true
	}

	// Stable index for creation-order tie-breaks (covers evidence children too).
	allIDs := make([]string, 0, len(nodeByID))
	for id := range nodeByID {
		allIDs = append(allIDs, id)
	}
	sort.Slice(allIDs, func(i, j int) bool {
		a, b := nodeByID[allIDs[i]], nodeByID[allIDs[j]]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	idx := make(map[string]int, len(allIDs))
	for i, id := range allIDs {
		idx[id] = i
	}

	var build func(id, parentID string, depth int) *TreeNode
	build = func(id, parentID string, depth int) *TreeNode {
		refs := childrenOf[id]
		sort.SliceStable(refs, func(i, j int) bool {
			if refs[i].weight != refs[j].weight {
				return refs[i].weight < refs[j].weight
			}
			return idx[refs[i].id] < idx[refs[j].id]
		})
		tn := &TreeNode{Node: nodeByID[id], ParentID: parentID, Depth: depth}
		for _, r := range refs {
			tn.Children = append(tn.Children, build(r.id, id, depth+1))
		}
		return tn
	}

	var roots []*TreeNode
	for _, id := range order {
		if !hasParent[id] {
			roots = append(roots, build(id, "", 0))
		}
	}
	return roots
}

// Flatten walks the forest depth-first, returning bullets in visual order.
func Flatten(forest []*TreeNode) []*TreeNode {
	var out []*TreeNode
	var walk func(ns []*TreeNode)
	walk = func(ns []*TreeNode) {
		for _, n := range ns {
			out = append(out, n)
			walk(n.Children)
		}
	}
	walk(forest)
	return out
}

// Find returns the TreeNode with the given node ID, or nil.
func Find(forest []*TreeNode, id string) *TreeNode {
	for _, tn := range Flatten(forest) {
		if tn.Node.ID == id {
			return tn
		}
	}
	return nil
}

// Neighbors returns the node IDs immediately before and after id in visual
// (flattened) order — used to render data-prev-id / data-next-id for keyboard
// navigation. Empty string means "none".
func Neighbors(forest []*TreeNode, id string) (prev, next string) {
	flat := Flatten(forest)
	for i, tn := range flat {
		if tn.Node.ID != id {
			continue
		}
		if i > 0 {
			prev = flat[i-1].Node.ID
		}
		if i+1 < len(flat) {
			next = flat[i+1].Node.ID
		}
		return prev, next
	}
	return "", ""
}
