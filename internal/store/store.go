package store

import "github.com/biorisk/flying-shuttle/internal/model"

// Store defines persistence operations for all domain entities.
type Store interface {
	Migrate() error
	Close() error

	// Chunks (immutable — no Update or Delete)
	CreateChunk(c *model.Chunk) error
	GetChunk(id string) (*model.Chunk, error)
	ListChunks() ([]model.Chunk, error)

	// Nodes
	CreateNode(n *model.Node) error
	GetNode(id string) (*model.Node, error)
	ListNodes() ([]model.Node, error)
	UpdateNode(n *model.Node) error // checks version for optimistic concurrency
	DeleteNode(id string) error

	// Node ↔ Chunk associations
	GetNodeChunks(nodeID string) ([]model.Chunk, error)
	SetNodeChunks(nodeID string, chunkIDs []string) error

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
	UpdateUploadStatus(id string, status model.UploadStatus, errMsg string) error

	// Transcript segments
	CreateTranscriptSegment(seg *model.TranscriptSegment) error
	ListTranscriptSegments(uploadID string) ([]model.TranscriptSegment, error)
}
