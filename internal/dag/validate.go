package dag

import (
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
)

// WouldCreateCycle returns true if adding an edge from→to would create a cycle.
// It performs a DFS from "to" following existing edges; if "from" is reachable
// from "to", adding from→to would close a loop.
func WouldCreateCycle(s store.Store, from, to string) (bool, error) {
	visited := map[string]bool{}
	return dfsReaches(s, to, from, visited)
}

// dfsReaches returns true if target is reachable from current via outgoing edges.
func dfsReaches(s store.Store, current, target string, visited map[string]bool) (bool, error) {
	if current == target {
		return true, nil
	}
	if visited[current] {
		return false, nil
	}
	visited[current] = true

	edges, err := s.ListEdgesFrom(current)
	if err != nil {
		return false, err
	}
	for _, e := range edges {
		found, err := dfsReaches(s, e.ToNode, target, visited)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// FindRoots returns all nodes with no incoming edges (DAG entry points).
func FindRoots(s store.Store) ([]model.Node, error) {
	nodes, err := s.ListNodes()
	if err != nil {
		return nil, err
	}
	edges, err := s.ListEdges()
	if err != nil {
		return nil, err
	}

	hasIncoming := map[string]bool{}
	for _, e := range edges {
		hasIncoming[e.ToNode] = true
	}

	var roots []model.Node
	for _, n := range nodes {
		if !hasIncoming[n.ID] {
			roots = append(roots, n)
		}
	}
	return roots, nil
}
