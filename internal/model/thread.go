package model

import "time"

type Thread struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ThreadNode represents a node's membership and ordering within a thread.
type ThreadNode struct {
	ThreadID string `json:"thread_id"`
	NodeID   string `json:"node_id"`
	Position int    `json:"position"`
}
