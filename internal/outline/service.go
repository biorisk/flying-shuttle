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

// SetTitle updates a bullet's title. version, when > 0, is checked for
// optimistic concurrency (store.ErrConflict on mismatch).
func (s *Service) SetTitle(id, title string, version int) (*model.Node, error) {
	n, err := s.Store.GetNode(id)
	if err != nil {
		return nil, err
	}
	if n.Title == title {
		return n, nil
	}
	n.Title = title
	if version > 0 {
		n.Version = version
	}
	if err := s.Store.UpdateNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

// FocusAfterDelete returns the bullet id to focus once id is deleted: the
// bullet visually before it, else the one after, else "".
func (s *Service) FocusAfterDelete(id string) (string, error) {
	forest, err := s.Tree()
	if err != nil {
		return "", err
	}
	flat := Flatten(forest)
	for i, tn := range flat {
		if tn.Node.ID != id {
			continue
		}
		if i > 0 {
			return flat[i-1].Node.ID, nil
		}
		if i+1 < len(flat) {
			return flat[i+1].Node.ID, nil
		}
		return "", nil
	}
	return "", nil
}

// AttachEvidence creates a locked chunk_ref sub-bullet under parentID holding
// the given passage, and records the evidence row linking it to its source
// chunk. When text is empty (or the range is degenerate) the whole chunk is
// attached. Returns the new evidence bullet.
func (s *Service) AttachEvidence(parentID, chunkID string, charStart, charEnd int, text string) (*model.Node, error) {
	chunk, err := s.Store.GetChunk(chunkID)
	if err != nil {
		return nil, err
	}
	runes := []rune(chunk.Content)
	if text == "" || charEnd <= charStart || charStart < 0 || charEnd > len(runes) {
		charStart, charEnd, text = 0, len(runes), chunk.Content
	}

	_, parent, err := s.treeAndNode(parentID)
	if err != nil {
		return nil, err
	}

	node := &model.Node{
		ID:     uuid.NewString(),
		Type:   model.NodeTypeChunkRef,
		Title:  previewText(text, 80),
		Body:   text,
		Locked: true,
		Labels: map[string]string{"source_file": chunk.SourceFile},
	}
	if err := s.Store.CreateNode(node); err != nil {
		return nil, err
	}
	if err := s.Store.MoveNode(node.ID, parentID, len(parent.Children)); err != nil {
		return nil, err
	}

	if err := s.Store.CreateEvidence(&model.Evidence{
		NodeID:     node.ID,
		ChunkID:    chunkID,
		SourceFile: chunk.SourceFile,
		CharStart:  charStart,
		CharEnd:    charEnd,
		Text:       text,
		Position:   0,
	}); err != nil {
		return nil, err
	}
	return node, nil
}

// SetLocked toggles a bullet's locked flag.
func (s *Service) SetLocked(id string, locked bool) (*model.Node, error) {
	n, err := s.Store.GetNode(id)
	if err != nil {
		return nil, err
	}
	n.Locked = locked
	if err := s.Store.UpdateNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

func previewText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// Move reparents nodeID under newParentID (empty = root) at the given sibling
// position. It refuses to move a node beneath itself or one of its own
// descendants (which would detach a subtree into a cycle-free orphan).
func (s *Service) Move(nodeID, newParentID string, position int) error {
	if nodeID == newParentID {
		return ErrNoop
	}
	forest, moving, err := s.treeAndNode(nodeID)
	if err != nil {
		return err
	}
	if newParentID != "" {
		if Find([]*TreeNode{moving}, newParentID) != nil {
			return ErrNoop // target is a descendant of the node being moved
		}
		if Find(forest, newParentID) == nil {
			return store.ErrNotFound
		}
	}
	if position < 0 {
		position = 0
	}
	return s.Store.MoveNode(nodeID, newParentID, position)
}
