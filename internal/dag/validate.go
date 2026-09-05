package dag

import (
	"fmt"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/doc"
)

// Issue represents a single graph integrity problem.
type Issue struct {
	Type    string `json:"type"` // "cycle", "self_link", "dangling_edge", "orphan_thread_ref"
	Message string `json:"message"`
	ID      string `json:"id"` // ID of the problematic entity
}

// Report is the result of a full graph validation.
type Report struct {
	Valid  bool    `json:"valid"`
	Issues []Issue `json:"issues,omitempty"`
}

// ValidateGraph performs comprehensive integrity checks on the entire graph:
//   - DAG acyclicity (via topological sort)
//   - Self-links (edge where from == to)
//   - Dangling edge references (edges pointing to non-existent nodes)
//   - Dangling thread node references (thread_nodes pointing to non-existent nodes)
func ValidateGraph(s doc.Store) (*Report, error) {
	report := &Report{Valid: true}

	nodes, err := s.ListNodes()
	if err != nil {
		return nil, err
	}
	edges, err := s.ListEdges()
	if err != nil {
		return nil, err
	}
	threads, err := s.ListThreads()
	if err != nil {
		return nil, err
	}

	nodeIDs := map[string]bool{}
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}

	// Check edges for self-links and dangling references.
	for _, e := range edges {
		if e.FromNode == e.ToNode {
			report.addIssue("self_link", e.ID,
				fmt.Sprintf("edge %s is a self-link (node %s)", e.ID, e.FromNode))
		}
		if !nodeIDs[e.FromNode] {
			report.addIssue("dangling_edge", e.ID,
				fmt.Sprintf("edge %s references non-existent from_node %s", e.ID, e.FromNode))
		}
		if !nodeIDs[e.ToNode] {
			report.addIssue("dangling_edge", e.ID,
				fmt.Sprintf("edge %s references non-existent to_node %s", e.ID, e.ToNode))
		}
	}

	// Check thread node references.
	for _, th := range threads {
		tns, err := s.GetThreadNodes(th.ID)
		if err != nil {
			return nil, err
		}
		for _, tn := range tns {
			if !nodeIDs[tn.NodeID] {
				report.addIssue("orphan_thread_ref", th.ID,
					fmt.Sprintf("thread %s references non-existent node %s at position %d", th.ID, tn.NodeID, tn.Position))
			}
		}
	}

	// Check for cycles via topological sort.
	_, err = TopologicalSort(s)
	if err != nil {
		report.addIssue("cycle", "", fmt.Sprintf("graph contains a cycle: %v", err))
	}

	return report, nil
}

func (r *Report) addIssue(typ, id, msg string) {
	r.Valid = false
	r.Issues = append(r.Issues, Issue{Type: typ, ID: id, Message: msg})
}

// WouldCreateCycle returns true if adding an edge from→to would create a cycle.
// It performs a DFS from "to" following existing edges; if "from" is reachable
// from "to", adding from→to would close a loop.
func WouldCreateCycle(s doc.Store, from, to string) (bool, error) {
	visited := map[string]bool{}
	return dfsReaches(s, to, from, visited)
}

// dfsReaches returns true if target is reachable from current via outgoing edges.
func dfsReaches(s doc.Store, current, target string, visited map[string]bool) (bool, error) {
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
func FindRoots(s doc.Store) ([]model.Node, error) {
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
