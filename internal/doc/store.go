package doc

import "github.com/biorisk/flying-shuttle/internal/model"

// Store defines persistence operations for the writing project: the outline
// (nodes / edges), threads, evidence, snapshots and branches. Corpus-owned
// data (chunks, uploads, transcript segments, embeddings) lives behind
// corpus.Store. A project row references the corpus only by chunk id.
type Store interface {
	Migrate() error
	Close() error

	// Nodes
	CreateNode(n *model.Node) error
	GetNode(id string) (*model.Node, error)
	ListNodes() ([]model.Node, error)
	UpdateNode(n *model.Node) error // checks version for optimistic concurrency
	DeleteNode(id string) error

	// Evidence: supporting text spans attached to a node. The row carries a
	// denormalized copy of its excerpt (source_file, char range, text) so it
	// renders without the corpus; chunk_id is the only corpus reference.
	CreateEvidence(e *model.Evidence) error
	UpdateEvidence(e *model.Evidence) error // updates char_start, char_end, text
	ListNodeEvidence(nodeID string) ([]model.Evidence, error)
	ListAllEvidence() ([]model.Evidence, error)
	DeleteEvidence(id string) error
	DeleteNodeEvidence(nodeID string) error

	// Node move (atomic reparent + reorder)
	MoveNode(nodeID, newParentID string, position int) error

	// Edges
	CreateEdge(e *model.Edge) error
	GetEdge(id string) (*model.Edge, error)
	ListEdges() ([]model.Edge, error)
	ListEdgesFrom(nodeID string) ([]model.Edge, error)
	ListEdgesTo(nodeID string) ([]model.Edge, error)
	DeleteEdge(id string) error

	// Threads
	CreateThread(t *model.Thread) error
	GetThread(id string) (*model.Thread, error)
	ListThreads() ([]model.Thread, error)
	UpdateThread(t *model.Thread) error
	DeleteThread(id string) error

	// Thread ↔ Node ordering
	GetThreadNodes(threadID string) ([]model.ThreadNode, error)
	SetThreadNodes(threadID string, nodes []model.ThreadNode) error

	// Full DAG state (for the working-doc mirror / recovery)
	ExportState() (*model.SnapshotData, error)
	ImportState(data *model.SnapshotData) error

	// Snapshots
	CreateSnapshot(label string) (*model.SnapshotSummary, error)
	GetSnapshot(id string) (*model.Snapshot, error)
	ListSnapshots() ([]model.SnapshotSummary, error)
	DeleteSnapshot(id string) error
	RestoreSnapshot(id string) error

	// Branches
	CreateBranch(name string) (*model.BranchSummary, error)
	GetBranch(id string) (*model.Branch, error)
	ListBranches() ([]model.BranchSummary, error)
	UpdateBranch(id string, name string) (*model.BranchSummary, error)
	DeleteBranch(id string) error
	SwitchBranch(id string) error
	GetActiveBranch() (*model.BranchSummary, error)
}
