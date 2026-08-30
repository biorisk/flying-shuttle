package outline

import "github.com/biorisk/flying-shuttle/internal/model"

// DiffStatus marks how a surviving bullet changed relative to a baseline.
type DiffStatus string

const (
	DiffAdded   DiffStatus = "added"
	DiffChanged DiffStatus = "changed"
)

// Ghost is a bullet that existed in the baseline but not in the current
// outline. ParentID is its nearest baseline ancestor that still exists
// ("" = root); Weight is its original sibling weight there.
type Ghost struct {
	Node     model.Node
	ParentID string
	Weight   int
}

// DiffResult is the outcome of comparing the current outline to a baseline.
type DiffResult struct {
	Status map[string]DiffStatus // current node id -> added | changed
	Ghosts []Ghost
}

// Diff compares the current outline against a baseline (a snapshot's or
// branch's nodes+edges). Go port of the former client `computeDiff`:
//   - added:   outline node present now, absent in baseline
//   - changed: present in both, differing title / body / locked
//   - removed: present in baseline, absent now -> a Ghost anchored to its
//     nearest still-existing baseline ancestor
func Diff(curNodes []model.Node, _ []model.Edge, baseNodes []model.Node, baseEdges []model.Edge) DiffResult {
	cur := outlineByID(curNodes)
	base := outlineByID(baseNodes)

	res := DiffResult{Status: map[string]DiffStatus{}}

	for id := range cur {
		if _, ok := base[id]; !ok {
			res.Status[id] = DiffAdded
		}
	}
	for id, c := range cur {
		if b, ok := base[id]; ok {
			if c.Title != b.Title || c.Body != b.Body || c.Locked != b.Locked {
				res.Status[id] = DiffChanged
			}
		}
	}

	// Baseline parent index (linear edges only).
	type pw struct {
		parent string
		weight int
	}
	baseParent := map[string]pw{}
	for _, e := range baseEdges {
		if e.Type != model.EdgeTypeLinear {
			continue
		}
		if _, ok := base[e.FromNode]; !ok {
			continue
		}
		if _, ok := base[e.ToNode]; !ok {
			continue
		}
		baseParent[e.ToNode] = pw{e.FromNode, e.Weight}
	}

	for id, bn := range base {
		if _, ok := cur[id]; ok {
			continue
		}
		p := baseParent[id]
		// Walk up until we hit an ancestor that still exists.
		anchor := ""
		cursor := p.parent
		for cursor != "" {
			if _, ok := cur[cursor]; ok {
				anchor = cursor
				break
			}
			cursor = baseParent[cursor].parent
		}
		res.Ghosts = append(res.Ghosts, Ghost{Node: bn, ParentID: anchor, Weight: p.weight})
	}
	return res
}

func outlineByID(nodes []model.Node) map[string]model.Node {
	m := make(map[string]model.Node)
	for _, n := range nodes {
		if n.Type == model.NodeTypeOutline {
			m[n.ID] = n
		}
	}
	return m
}
