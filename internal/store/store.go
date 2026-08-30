package store

import "github.com/biorisk/flying-shuttle/internal/model"

// Store defines persistence operations for all domain entities.
type Store interface {
	Migrate() error
	Close() error

	// Chunks (content is immutable; the embedding vector is derived metadata
	// and may be filled in later via SetChunkEmbedding)
	CreateChunk(c *model.Chunk) error
	CreateChunks(chunks []model.Chunk) error // batch insert in a single transaction
	GetChunk(id string) (*model.Chunk, error)
	ListChunks() ([]model.Chunk, error)
	// ListChunksPage returns a page (ordered by created_at) plus the total count.
	// limit <= 0 means unlimited.
	ListChunksPage(limit, offset int) ([]model.Chunk, int, error)
	ListChunksBySourceFile(sourceFile string) ([]model.Chunk, error)
	ListChunkIDs() ([]string, error)
	ListChunkIDsWithEmbedding() ([]string, error)
	GetChunksByIDs(ids []string) ([]model.Chunk, error)
	ListChunksMissingEmbedding(limit int) ([]model.Chunk, error)
	CountChunksMissingEmbedding() (int, error)
	SetChunkEmbedding(id string, vec []byte) error

	// Nodes
	CreateNode(n *model.Node) error
	GetNode(id string) (*model.Node, error)
	ListNodes() ([]model.Node, error)
	UpdateNode(n *model.Node) error // checks version for optimistic concurrency
	DeleteNode(id string) error

	// Node ↔ Chunk associations (legacy whole-chunk model)
	GetNodeChunks(nodeID string) ([]model.Chunk, error)
	SetNodeChunks(nodeID string, chunkIDs []string) error
	ListUsedChunkIDs() ([]string, error) // all chunk IDs referenced by node_chunks or evidence

	// Evidence: supporting text spans attached to a node (supersedes node_chunks)
	CreateEvidence(e *model.Evidence) error
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

	// Uploads
	CreateUpload(u *model.Upload) error
	GetUpload(id string) (*model.Upload, error)
	ListUploads() ([]model.Upload, error)
	// ListUploadsPage returns a page (newest first) plus the total count.
	// limit <= 0 means unlimited.
	ListUploadsPage(limit, offset int) ([]model.Upload, int, error)
	UpdateUploadStatus(id string, status model.UploadStatus, errMsg string) error

	// Transcript segments
	CreateTranscriptSegment(seg *model.TranscriptSegment) error
	ListTranscriptSegments(uploadID string) ([]model.TranscriptSegment, error)

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
