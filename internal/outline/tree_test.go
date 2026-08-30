package outline

import (
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
)

func n(id string, created int) model.Node {
	return model.Node{
		ID: id, Type: model.NodeTypeOutline, Title: id,
		CreatedAt: time.Unix(int64(created), 0),
	}
}

func linear(from, to string, weight int) model.Edge {
	return model.Edge{ID: from + "->" + to, FromNode: from, ToNode: to, Type: model.EdgeTypeLinear, Weight: weight}
}

func ids(ns []*TreeNode) []string {
	out := make([]string, len(ns))
	for i, x := range ns {
		out[i] = x.Node.ID
	}
	return out
}

func TestBuildTree_rootsInCreationOrder(t *testing.T) {
	nodes := []model.Node{n("b", 2), n("a", 1), n("c", 3)}
	forest := BuildTree(nodes, nil)
	if got := ids(forest); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("roots not in creation order: %v", got)
	}
}

func TestBuildTree_childrenByWeight(t *testing.T) {
	nodes := []model.Node{n("root", 1), n("x", 2), n("y", 3), n("z", 4)}
	edges := []model.Edge{
		linear("root", "y", 5),
		linear("root", "x", 1),
		linear("root", "z", 3),
	}
	forest := BuildTree(nodes, edges)
	if len(forest) != 1 {
		t.Fatalf("expected 1 root, got %d", len(forest))
	}
	if got := ids(forest[0].Children); got[0] != "x" || got[1] != "z" || got[2] != "y" {
		t.Fatalf("children not weight-ordered: %v", got)
	}
	if forest[0].Children[0].Depth != 1 || forest[0].Children[0].ParentID != "root" {
		t.Fatalf("depth/parent wrong: %+v", forest[0].Children[0])
	}
}

func TestBuildTree_ignoresNonOutlineAndNonLinear(t *testing.T) {
	nodes := []model.Node{
		n("root", 1),
		n("child", 2),
		{ID: "ref", Type: model.NodeTypeChunkRef, CreatedAt: time.Unix(3, 0)},
	}
	edges := []model.Edge{
		linear("root", "child", 0),
		{ID: "b", FromNode: "root", ToNode: "child", Type: model.EdgeTypeBranch},
		linear("root", "ref", 1), // ref is chunk_ref -> excluded
	}
	forest := BuildTree(nodes, edges)
	if len(forest) != 1 || len(forest[0].Children) != 1 || forest[0].Children[0].Node.ID != "child" {
		t.Fatalf("unexpected tree: %v", ids(Flatten(forest)))
	}
}

func TestNeighbors(t *testing.T) {
	nodes := []model.Node{n("a", 1), n("b", 2), n("c", 3)}
	edges := []model.Edge{linear("a", "b", 0)} // a > b (child), then c (root)
	forest := BuildTree(nodes, edges)
	// visual order: a, b, c
	if p, nx := Neighbors(forest, "b"); p != "a" || nx != "c" {
		t.Fatalf("b neighbors: %q %q", p, nx)
	}
	if p, nx := Neighbors(forest, "a"); p != "" || nx != "b" {
		t.Fatalf("a neighbors: %q %q", p, nx)
	}
	if p, nx := Neighbors(forest, "c"); p != "b" || nx != "" {
		t.Fatalf("c neighbors: %q %q", p, nx)
	}
}
