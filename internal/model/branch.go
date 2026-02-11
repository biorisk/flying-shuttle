package model

import "time"

// Branch is the full branch record including the serialized DAG state.
type Branch struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Data      SnapshotData `json:"data"`
	Active    bool         `json:"active"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// BranchSummary is the listing view (no data blob).
type BranchSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
