package model

import "time"

type Chunk struct {
	ID           string  `json:"id"`
	SourceFile   string  `json:"source_file"`
	Content      string  `json:"content"`
	StartOffset  int     `json:"start_offset"`
	EndOffset    int     `json:"end_offset"`
	Speaker      *string `json:"speaker,omitempty"`
	EmbeddingVec []byte  `json:"embedding_vec,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
