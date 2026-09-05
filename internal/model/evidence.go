package model

import "time"

// Evidence is a supporting passage attached to a node: a text span drawn from
// a transcript chunk. A whole-chunk attachment has CharStart == 0 and
// CharEnd == len([]rune(chunk.Content)); a sub-span narrows that range. Text
// is the resolved excerpt, stored verbatim so stitching never re-resolves it.
type Evidence struct {
	ID         string    `json:"id"`
	NodeID     string    `json:"node_id"`
	ChunkID    string    `json:"chunk_id"`
	SourceFile string    `json:"source_file"`
	CharStart  int       `json:"char_start"`
	CharEnd    int       `json:"char_end"`
	Text       string    `json:"text"`
	Position   int       `json:"position"`
	Edited     bool      `json:"edited"`
	CreatedAt  time.Time `json:"created_at"`
}
