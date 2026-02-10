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

// ConditionalWalk performs a DFS from the given start nodes, following only
// edges whose conditions are satisfied by the given EvalContext. Nodes are
// visited in the order they are reached, skipping already-visited nodes.
// This enables audience-aware navigation through the DAG.
func ConditionalWalk(s store.Store, startIDs []string, ctx *EvalContext) ([]model.Node, error) {
	if ctx == nil {
		ctx = &EvalContext{}
	}
	if ctx.ReadNodes == nil {
		ctx.ReadNodes = make(map[string]bool)
	}

	var result []model.Node
	visited := make(map[string]bool)

	var walk func(id string) error
	walk = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true

		n, err := s.GetNode(id)
		if err != nil {
			return fmt.Errorf("node %s: %w", id, err)
		}
		result = append(result, *n)
		ctx.ReadNodes[id] = true

		// Follow outgoing edges, filtered by condition and sorted by weight.
		edges, err := s.ListEdgesFrom(id)
		if err != nil {
			return fmt.Errorf("edges from %s: %w", id, err)
		}
		for _, e := range edges {
			cond := ""
			if e.Condition != nil {
				cond = *e.Condition
			}
			if EvalCondition(cond, ctx) {
				if err := walk(e.ToNode); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, id := range startIDs {
		if err := walk(id); err != nil {
			return nil, err
		}
	}
	return result, nil
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
