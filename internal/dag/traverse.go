package dag

import (
	"fmt"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
)

// Linearize walks a thread's ordered nodes and returns them with full node data.
func Linearize(s store.Store, threadID string) ([]model.Node, error) {
	tns, err := s.GetThreadNodes(threadID)
	if err != nil {
		return nil, err
	}
	nodes := make([]model.Node, 0, len(tns))
	for _, tn := range tns {
		n, err := s.GetNode(tn.NodeID)
		if err != nil {
			return nil, fmt.Errorf("node %s in thread: %w", tn.NodeID, err)
		}
		nodes = append(nodes, *n)
	}
	return nodes, nil
}

// TopologicalSort returns all nodes in topological order (Kahn's algorithm).
// Returns an error if the graph contains a cycle.
func TopologicalSort(s store.Store) ([]model.Node, error) {
	allNodes, err := s.ListNodes()
	if err != nil {
		return nil, err
	}
	allEdges, err := s.ListEdges()
	if err != nil {
		return nil, err
	}

	nodeMap := map[string]model.Node{}
	inDegree := map[string]int{}
	adj := map[string][]string{}

	for _, n := range allNodes {
		nodeMap[n.ID] = n
		inDegree[n.ID] = 0
	}
	for _, e := range allEdges {
		adj[e.FromNode] = append(adj[e.FromNode], e.ToNode)
		inDegree[e.ToNode]++
	}

	// Collect roots (in-degree 0).
	var queue []string
	for _, n := range allNodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	var sorted []model.Node
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, nodeMap[id])

		for _, child := range adj[id] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(sorted) != len(allNodes) {
		return nil, fmt.Errorf("cycle detected: sorted %d of %d nodes", len(sorted), len(allNodes))
	}
	return sorted, nil
}
