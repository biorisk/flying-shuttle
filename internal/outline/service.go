package outline

import (
	"errors"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/google/uuid"
)

// ErrNoop is returned by structural operations that would have no effect
// (indenting the first sibling, unindenting a root). Callers treat it as a
// silent success.
var ErrNoop = errors.New("outline: operation has no effect")

// Service composes store primitives into the structural edits the outline
// editor performs. Each op reads the current tree, then applies the change
// via the store's atomic MoveNode / CreateNode. A crash between the two
// leaves at worst an orphan root node (recoverable), never a corrupt graph.
type Service struct {
	Store store.Store
}

// Tree returns the current outline forest.
func (s *Service) Tree() ([]*TreeNode, error) {
	nodes, err := s.Store.ListNodes()
	if err != nil {
		return nil, err
	}
	edges, err := s.Store.ListEdges()
	if err != nil {
		return nil, err
	}
	return BuildTree(nodes, edges), nil
}

func (s *Service) treeAndNode(id string) ([]*TreeNode, *TreeNode, error) {
	forest, err := s.Tree()
	if err != nil {
		return nil, nil, err
	}
	tn := Find(forest, id)
	if tn == nil {
		return nil, nil, store.ErrNotFound
	}
	return forest, tn, nil
}

// siblingsOf returns the ordered sibling TreeNodes of the node with the given
// parentID within forest (roots when parentID == "").
func siblingsOf(forest []*TreeNode, parentID string) []*TreeNode {
	if parentID == "" {
		return forest
	}
	if p := Find(forest, parentID); p != nil {
		return p.Children
	}
	return nil
}

func indexOf(siblings []*TreeNode, id string) int {
	for i, tn := range siblings {
		if tn.Node.ID == id {
			return i
		}
	}
	return -1
}

// AddRoot creates a new empty outline node with no parent.
func (s *Service) AddRoot(title string) (*model.Node, error) {
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: title}
	if err := s.Store.CreateNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

// AddChild creates a new outline node as the last child of parentID.
func (s *Service) AddChild(parentID, title string) (*model.Node, error) {
	_, parent, err := s.treeAndNode(parentID)
	if err != nil {
		return nil, err
	}
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: title}
	if err := s.Store.CreateNode(n); err != nil {
		return nil, err
	}
	if err := s.Store.MoveNode(n.ID, parentID, len(parent.Children)); err != nil {
		return nil, err
	}
	return n, nil
}

// AddChildAt creates a new outline node as a child of parentID at position.
func (s *Service) AddChildAt(parentID, title string, position int) (*model.Node, error) {
	if _, _, err := s.treeAndNode(parentID); err != nil {
		return nil, err
	}
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: title}
	if err := s.Store.CreateNode(n); err != nil {
		return nil, err
	}
	if err := s.Store.MoveNode(n.ID, parentID, position); err != nil {
		return nil, err
	}
	return n, nil
}

// AddSibling creates a new outline node immediately after afterID, under the
// same parent. If afterID is a root, the new node is also a root (appended
// after it in creation order — it carries no linear edge).
func (s *Service) AddSibling(afterID, title string) (*model.Node, error) {
	forest, after, err := s.treeAndNode(afterID)
	if err != nil {
		return nil, err
	}
	n := &model.Node{ID: uuid.NewString(), Type: model.NodeTypeOutline, Title: title}
	if err := s.Store.CreateNode(n); err != nil {
		return nil, err
	}
	if after.ParentID == "" {
		return n, nil // sibling of a root is just another root
	}
	siblings := siblingsOf(forest, after.ParentID)
	pos := indexOf(siblings, afterID) + 1
	if err := s.Store.MoveNode(n.ID, after.ParentID, pos); err != nil {
		return nil, err
	}
	return n, nil
}

// Indent makes nodeID a child of its immediately-preceding sibling. No-op for
// the first sibling.
func (s *Service) Indent(nodeID string) (*model.Node, error) {
	forest, tn, err := s.treeAndNode(nodeID)
	if err != nil {
		return nil, err
	}
	siblings := siblingsOf(forest, tn.ParentID)
	i := indexOf(siblings, nodeID)
	if i <= 0 {
		return nil, ErrNoop
	}
	newParent := siblings[i-1]
	if err := s.Store.MoveNode(nodeID, newParent.Node.ID, len(newParent.Children)); err != nil {
		return nil, err
	}
	return &tn.Node, nil
}

// Unindent moves nodeID up one level, placing it right after its former
// parent among the grandparent's children. No-op for a root.
func (s *Service) Unindent(nodeID string) (*model.Node, error) {
	forest, tn, err := s.treeAndNode(nodeID)
	if err != nil {
		return nil, err
	}
	if tn.ParentID == "" {
		return nil, ErrNoop
	}
	parent := Find(forest, tn.ParentID)
	grandparentID := parent.ParentID
	gpSiblings := siblingsOf(forest, grandparentID)
	parentIdx := indexOf(gpSiblings, parent.Node.ID)
	if err := s.Store.MoveNode(nodeID, grandparentID, parentIdx+1); err != nil {
		return nil, err
	}
	return &tn.Node, nil
}

// Delete removes a node. Its former children lose their incoming edge and
// become roots (matching the pre-existing DeleteNode behavior).
func (s *Service) Delete(nodeID string) error {
	return s.Store.DeleteNode(nodeID)
}
