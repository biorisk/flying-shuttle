package outline

import (
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
)

func on(id, title string) model.Node {
	return model.Node{ID: id, Type: model.NodeTypeOutline, Title: title, CreatedAt: time.Unix(1, 0)}
}

func TestDiff_addedChangedRemoved(t *testing.T) {
	base := []model.Node{on("a", "A"), on("b", "B"), on("c", "C")}
	baseEdges := []model.Edge{
		{ID: "1", FromNode: "a", ToNode: "b", Type: model.EdgeTypeLinear, Weight: 0},
		{ID: "2", FromNode: "b", ToNode: "c", Type: model.EdgeTypeLinear, Weight: 0},
	}
	// current: a unchanged, b retitled, c removed, d added under a
	cur := []model.Node{on("a", "A"), on("b", "B-renamed"), on("d", "D")}
	curEdges := []model.Edge{
		{ID: "3", FromNode: "a", ToNode: "b", Type: model.EdgeTypeLinear},
		{ID: "4", FromNode: "a", ToNode: "d", Type: model.EdgeTypeLinear},
	}

	res := Diff(cur, curEdges, base, baseEdges)

	if res.Status["d"] != DiffAdded {
		t.Fatalf("d should be added: %v", res.Status)
	}
	if res.Status["b"] != DiffChanged {
		t.Fatalf("b should be changed: %v", res.Status)
	}
	if _, ok := res.Status["a"]; ok {
		t.Fatalf("a should be unchanged")
	}
	if len(res.Ghosts) != 1 || res.Ghosts[0].Node.ID != "c" {
		t.Fatalf("c should be a ghost: %+v", res.Ghosts)
	}
	// c's baseline parent b still exists -> anchor is b
	if res.Ghosts[0].ParentID != "b" {
		t.Fatalf("ghost anchor should be b, got %q", res.Ghosts[0].ParentID)
	}
}

func TestDiff_ghostAnchorWalksUp(t *testing.T) {
	base := []model.Node{on("a", "A"), on("b", "B"), on("c", "C")}
	baseEdges := []model.Edge{
		{ID: "1", FromNode: "a", ToNode: "b", Type: model.EdgeTypeLinear},
		{ID: "2", FromNode: "b", ToNode: "c", Type: model.EdgeTypeLinear},
	}
	cur := []model.Node{on("a", "A")} // b and c both removed
	res := Diff(cur, nil, base, baseEdges)
	byID := map[string]Ghost{}
	for _, g := range res.Ghosts {
		byID[g.Node.ID] = g
	}
	if byID["c"].ParentID != "a" {
		t.Fatalf("c ghost should anchor to a (nearest survivor), got %q", byID["c"].ParentID)
	}
	if byID["b"].ParentID != "a" {
		t.Fatalf("b ghost should anchor to a, got %q", byID["b"].ParentID)
	}
}
