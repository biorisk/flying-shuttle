package model

import "time"

// EdgeType enumerates the kinds of directed edges.
type EdgeType string

const (
	EdgeTypeLinear EdgeType = "linear"
	EdgeTypeBranch EdgeType = "branch"
	EdgeTypeJump   EdgeType = "jump"
)

type Edge struct {
	ID        string   `json:"id"`
	FromNode  string   `json:"from_node"`
	ToNode    string   `json:"to_node"`
	Type      EdgeType `json:"type"`
	Condition *string  `json:"condition,omitempty"`
	Weight    int      `json:"weight"`
	CreatedAt time.Time `json:"created_at"`
}
