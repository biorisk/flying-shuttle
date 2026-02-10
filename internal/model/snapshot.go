package model

import "time"

type Snapshot struct {
	ID        string       `json:"id"`
	Label     string       `json:"label"`
	Data      SnapshotData `json:"data"`
	CreatedAt time.Time    `json:"created_at"`
}

// SnapshotSummary is the listing view (no data blob).
type SnapshotSummary struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// SnapshotData captures the full DAG state.
type SnapshotData struct {
	Nodes      []Node          `json:"nodes"`
	Edges      []Edge          `json:"edges"`
	Threads    []Thread        `json:"threads"`
	ThreadNodes []ThreadNode   `json:"thread_nodes"`
	NodeChunks []NodeChunkAssoc `json:"node_chunks"`
}

// NodeChunkAssoc records a node ↔ chunk association with ordering.
type NodeChunkAssoc struct {
	NodeID   string `json:"node_id"`
	ChunkID  string `json:"chunk_id"`
	Position int    `json:"position"`
}
