package model

import "time"

type UploadStatus string

const (
	UploadStatusPending      UploadStatus = "pending"
	UploadStatusTranscribing UploadStatus = "transcribing"
	UploadStatusDone         UploadStatus = "done"
	UploadStatusFailed       UploadStatus = "failed"
)

type Upload struct {
	ID        string       `json:"id"`
	Filename  string       `json:"filename"`
	Format    string       `json:"format"`
	SizeBytes int64        `json:"size_bytes"`
	Status    UploadStatus `json:"status"`
	Error     string       `json:"error,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// TranscriptSegment represents a diarized segment of a transcription.
type TranscriptSegment struct {
	ID        string    `json:"id"`
	UploadID  string    `json:"upload_id"`
	Speaker   string    `json:"speaker"`
	Text      string    `json:"text"`
	StartMs   int64     `json:"start_ms"`
	EndMs     int64     `json:"end_ms"`
	CreatedAt time.Time `json:"created_at"`
}
