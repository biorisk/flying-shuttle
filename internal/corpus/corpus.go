// Package corpus owns the shared, build-once half of a Flying Shuttle
// workspace: transcripts, chunks, embeddings, the search index, and the
// atlas. A writing project (internal/doc) references it only by chunk id.
//
// In Phase 1 of the corpus/project separation both stores are still backed
// by the same *sql.DB; corpus.Store is the vocabulary boundary, not yet a
// separate database.
package corpus

import (
	"database/sql"

	"github.com/biorisk/flying-shuttle/internal/model"
)

// Reader is the read-only subset of Store used by cross-boundary callers in
// the document half (attaching evidence, rendering candidate cards).
type Reader interface {
	GetChunk(id string) (*model.Chunk, error)
	GetChunksByIDs(ids []string) ([]model.Chunk, error)
	ListChunksBySourceFile(sourceFile string) ([]model.Chunk, error)
}

// Store is the full persistence surface for corpus-owned tables: chunks,
// uploads, transcript_segments and meta. The atlas keeps its own interface
// (internal/atlas) against DB().
type Store interface {
	Reader

	Migrate() error
	Close() error

	// Chunks (content is immutable; the embedding vector is derived
	// metadata and may be filled in later via SetChunkEmbedding).
	CreateChunk(c *model.Chunk) error
	CreateChunks(chunks []model.Chunk) error
	ListChunks() ([]model.Chunk, error)
	// SoftDeleteChunksBySourceFile marks every live chunk of one transcript
	// deleted (append-only corpus: re-ingest supersedes rather than mutates).
	SoftDeleteChunksBySourceFile(sourceFile string) (int, error)
	// ResolveChunk reports whether a chunk id exists at all and, if so,
	// whether it is soft-deleted. Used by `shuttle doctor` to tell a dangling
	// citation from one that merely cites a superseded chunk.
	ResolveChunk(id string) (found, deleted bool, err error)
	ListChunksPage(limit, offset int) ([]model.Chunk, int, error)
	ListChunkIDs() ([]string, error)
	ListChunkIDsWithEmbedding() ([]string, error)
	ListChunksMissingEmbedding(limit int) ([]model.Chunk, error)
	CountChunksMissingEmbedding() (int, error)
	SetChunkEmbedding(id string, vec []byte) error
	ClearAllEmbeddings() (int64, error)
	SampleEmbeddingDim() (int, error)

	// Uploads
	CreateUpload(u *model.Upload) error
	GetUpload(id string) (*model.Upload, error)
	ListUploads() ([]model.Upload, error)
	ListUploadsPage(limit, offset int) ([]model.Upload, int, error)
	UpdateUploadStatus(id string, status model.UploadStatus, errMsg string) error

	// Transcript segments
	CreateTranscriptSegment(seg *model.TranscriptSegment) error
	ListTranscriptSegments(uploadID string) ([]model.TranscriptSegment, error)

	// Meta (embed-model reconciliation)
	GetMeta(key string) (string, error)
	SetMeta(key, value string) error

	// DB exposes the underlying handle for self-contained subsystems
	// (internal/atlas) that own their own persistence.
	DB() *sql.DB
}
