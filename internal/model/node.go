package model

import "time"

// NodeType enumerates the kinds of nodes in the DAG.
type NodeType string

const (
	NodeTypeOutline  NodeType = "outline"
	NodeTypeChunkRef NodeType = "chunk_ref"
	NodeTypeSynth    NodeType = "synth"
)

type Node struct {
	ID        string            `json:"id"`
	Type      NodeType          `json:"type"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Labels    map[string]string `json:"labels,omitempty"`
	Locked    bool              `json:"locked"`
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
