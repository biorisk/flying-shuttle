package corpus

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
)

// sqlStore implements Store against a *sql.DB. The handle is injected: in
// Phase 1 it is the same connection the document store uses.
type sqlStore struct {
	db *sql.DB
}

// New wraps an existing database handle as a corpus.Store. It does not open
// or migrate anything — the caller owns the connection lifecycle.
func New(db *sql.DB) Store { return &sqlStore{db: db} }

var _ Store = (*sqlStore)(nil)

// DB returns the underlying database handle.
func (s *sqlStore) DB() *sql.DB { return s.db }

// --- Chunks (immutable) ---

func (s *sqlStore) CreateChunk(c *model.Chunk) error {
	now := time.Now().UTC()
	c.CreatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO chunks (id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.SourceFile, c.Content, c.StartOffset, c.EndOffset, c.Speaker, c.EmbeddingVec,
		now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *sqlStore) CreateChunks(chunks []model.Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	for i := range chunks {
		chunks[i].CreatedAt = now
		_, err := tx.Exec(
			`INSERT INTO chunks (id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			chunks[i].ID, chunks[i].SourceFile, chunks[i].Content,
			chunks[i].StartOffset, chunks[i].EndOffset, chunks[i].Speaker,
			chunks[i].EmbeddingVec, nowStr,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlStore) GetChunk(id string) (*model.Chunk, error) {
	row := s.db.QueryRow(
		`SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at FROM chunks WHERE id = ?`, id)
	return scanChunk(row)
}

func (s *sqlStore) ListChunks() ([]model.Chunk, error) {
	rows, err := s.db.Query(`SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at FROM chunks ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Chunk
	for rows.Next() {
		c, err := scanChunkRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ListChunksBySourceFile returns every chunk from one source transcript,
// ordered by position within that transcript (start_offset).
func (s *sqlStore) ListChunksBySourceFile(sourceFile string) ([]model.Chunk, error) {
	rows, err := s.db.Query(
		`SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at
		 FROM chunks WHERE source_file = ? ORDER BY start_offset, created_at`, sourceFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Chunk
	for rows.Next() {
		c, err := scanChunkRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ListChunksPage returns a page of chunks ordered by creation time, plus the
// total number of chunks in the store. A limit <= 0 means "no limit".
func (s *sqlStore) ListChunksPage(limit, offset int) ([]model.Chunk, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at FROM chunks ORDER BY created_at LIMIT ? OFFSET ?`
	lim := limit
	if lim <= 0 {
		lim = -1 // SQLite: negative LIMIT means no upper bound
	}
	rows, err := s.db.Query(q, lim, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.Chunk
	for rows.Next() {
		c, err := scanChunkRows(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// ListChunkIDs returns just the IDs of every chunk, cheaply (no content).
func (s *sqlStore) ListChunkIDs() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListChunkIDsWithEmbedding returns the IDs of chunks that have an embedding
// vector stored, cheaply (no content or vector data).
func (s *sqlStore) ListChunkIDsWithEmbedding() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM chunks WHERE embedding_vec IS NOT NULL AND length(embedding_vec) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetChunksByIDs returns the chunks with the given IDs, in no particular order.
func (s *sqlStore) GetChunksByIDs(ids []string) ([]model.Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]model.Chunk, 0, len(ids))
	const batch = 500 // keep well under SQLite's parameter limit
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
		}
		q := `SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at
		      FROM chunks WHERE id IN (` + strings.Join(ph, ",") + `)`
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			c, err := scanChunkRows(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, *c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// ListChunksMissingEmbedding returns up to limit chunks that have no embedding
// vector yet, oldest first. limit <= 0 returns all of them.
func (s *sqlStore) ListChunksMissingEmbedding(limit int) ([]model.Chunk, error) {
	q := `SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at
	      FROM chunks
	      WHERE embedding_vec IS NULL OR length(embedding_vec) = 0
	      ORDER BY created_at`
	var args []any
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Chunk
	for rows.Next() {
		c, err := scanChunkRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CountChunksMissingEmbedding returns how many chunks still lack an embedding.
func (s *sqlStore) CountChunksMissingEmbedding() (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT count(*) FROM chunks WHERE embedding_vec IS NULL OR length(embedding_vec) = 0`,
	).Scan(&n)
	return n, err
}

// SetChunkEmbedding attaches (or replaces) a chunk's embedding vector.
func (s *sqlStore) SetChunkEmbedding(id string, vec []byte) error {
	res, err := s.db.Exec(`UPDATE chunks SET embedding_vec = ? WHERE id = ?`, vec, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chunk %s: %w", id, ErrNotFound)
	}
	return nil
}

// ClearAllEmbeddings nulls every chunk's embedding vector. Used when the
// embedding model (and hence the vector space) changes.
func (s *sqlStore) ClearAllEmbeddings() (int64, error) {
	res, err := s.db.Exec(`UPDATE chunks SET embedding_vec = NULL WHERE embedding_vec IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SampleEmbeddingDim returns the float32 dimension of the first stored
// embedding vector, or 0 if none are stored.
func (s *sqlStore) SampleEmbeddingDim() (int, error) {
	var b []byte
	err := s.db.QueryRow(
		`SELECT embedding_vec FROM chunks WHERE embedding_vec IS NOT NULL LIMIT 1`).Scan(&b)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(b) / 4, nil
}

// --- Meta ---

// GetMeta returns meta[key], or "" if unset.
func (s *sqlStore) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetMeta upserts meta[key] = value.
func (s *sqlStore) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// --- Uploads ---

func (s *sqlStore) CreateUpload(u *model.Upload) error {
	now := time.Now().UTC()
	u.Status = model.UploadStatusPending
	u.CreatedAt = now
	u.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO uploads (id, filename, format, size_bytes, status, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Filename, u.Format, u.SizeBytes, string(u.Status), u.Error,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *sqlStore) GetUpload(id string) (*model.Upload, error) {
	row := s.db.QueryRow(
		`SELECT id, filename, format, size_bytes, status, error, created_at, updated_at FROM uploads WHERE id = ?`, id)
	return scanUpload(row)
}

func (s *sqlStore) ListUploads() ([]model.Upload, error) {
	rows, err := s.db.Query(
		`SELECT id, filename, format, size_bytes, status, error, created_at, updated_at FROM uploads ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ListUploadsPage returns a page of uploads (newest first) plus the total
// upload count. A limit <= 0 means "no limit".
func (s *sqlStore) ListUploadsPage(limit, offset int) ([]model.Upload, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM uploads`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if offset < 0 {
		offset = 0
	}
	lim := limit
	if lim <= 0 {
		lim = -1
	}
	rows, err := s.db.Query(
		`SELECT id, filename, format, size_bytes, status, error, created_at, updated_at FROM uploads ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		lim, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

func (s *sqlStore) UpdateUploadStatus(id string, status model.UploadStatus, errMsg string) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE uploads SET status=?, error=?, updated_at=? WHERE id=?`,
		string(status), errMsg, now.Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Transcript Segments ---

func (s *sqlStore) CreateTranscriptSegment(seg *model.TranscriptSegment) error {
	now := time.Now().UTC()
	seg.CreatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO transcript_segments (id, upload_id, speaker, text, start_ms, end_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		seg.ID, seg.UploadID, seg.Speaker, seg.Text, seg.StartMs, seg.EndMs,
		now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *sqlStore) ListTranscriptSegments(uploadID string) ([]model.TranscriptSegment, error) {
	rows, err := s.db.Query(
		`SELECT id, upload_id, speaker, text, start_ms, end_ms, created_at
		 FROM transcript_segments WHERE upload_id = ? ORDER BY start_ms`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TranscriptSegment
	for rows.Next() {
		var seg model.TranscriptSegment
		var ts string
		if err := rows.Scan(&seg.ID, &seg.UploadID, &seg.Speaker, &seg.Text, &seg.StartMs, &seg.EndMs, &ts); err != nil {
			return nil, err
		}
		seg.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, seg)
	}
	return out, rows.Err()
}

// --- scan helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanChunk(sc scanner) (*model.Chunk, error) {
	var c model.Chunk
	var ts string
	if err := sc.Scan(&c.ID, &c.SourceFile, &c.Content, &c.StartOffset, &c.EndOffset, &c.Speaker, &c.EmbeddingVec, &ts); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
	return &c, nil
}

func scanChunkRows(r *sql.Rows) (*model.Chunk, error) { return scanChunk(r) }

func scanUpload(sc scanner) (*model.Upload, error) {
	var u model.Upload
	var createdAt, updatedAt string
	if err := sc.Scan(&u.ID, &u.Filename, &u.Format, &u.SizeBytes, &u.Status, &u.Error, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &u, nil
}
