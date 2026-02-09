package ingest

import (
	"context"

	"github.com/biorisk/flying-shuttle/internal/model"
)

// TranscriptResult holds the segments returned by a transcription provider.
type TranscriptResult struct {
	Segments []model.TranscriptSegment
}

// Transcriber converts an audio file into diarized transcript segments.
type Transcriber interface {
	Transcribe(ctx context.Context, filePath string) (*TranscriptResult, error)
}

// StubTranscriber is a no-op implementation that returns an empty transcript.
// Replace with a real provider (e.g. Whisper, AssemblyAI) when ready.
type StubTranscriber struct{}

func (s *StubTranscriber) Transcribe(_ context.Context, _ string) (*TranscriptResult, error) {
	return &TranscriptResult{}, nil
}
