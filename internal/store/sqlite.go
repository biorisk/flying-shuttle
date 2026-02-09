package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at dsn.
// Use ":memory:" for an in-memory database (useful for tests).
func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	// Enable WAL mode and foreign keys.
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", pragma, err)
		}
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) Migrate() error {
	migrations := []string{
		"migrations/001_initial_schema.sql",
		"migrations/002_uploads.sql",
	}
	for _, name := range migrations {
		data, err := migrationFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := s.db.Exec(string(data)); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
	}
	return nil
}

// --- Chunks (immutable) ---

func (s *SQLiteStore) CreateChunk(c *model.Chunk) error {
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

func (s *SQLiteStore) GetChunk(id string) (*model.Chunk, error) {
	row := s.db.QueryRow(
		`SELECT id, source_file, content, start_offset, end_offset, speaker, embedding_vec, created_at FROM chunks WHERE id = ?`, id)
	return scanChunk(row)
}

func (s *SQLiteStore) ListChunks() ([]model.Chunk, error) {
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

// --- Nodes ---

func (s *SQLiteStore) CreateNode(n *model.Node) error {
	now := time.Now().UTC()
	n.Version = 1
	n.CreatedAt = now
	n.UpdatedAt = now
	labels, _ := json.Marshal(n.Labels)
	_, err := s.db.Exec(
		`INSERT INTO nodes (id, type, title, body, labels, locked, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, string(n.Type), n.Title, n.Body, string(labels), boolToInt(n.Locked), n.Version,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetNode(id string) (*model.Node, error) {
	row := s.db.QueryRow(
		`SELECT id, type, title, body, labels, locked, version, created_at, updated_at FROM nodes WHERE id = ?`, id)
	return scanNode(row)
}

func (s *SQLiteStore) ListNodes() ([]model.Node, error) {
	rows, err := s.db.Query(`SELECT id, type, title, body, labels, locked, version, created_at, updated_at FROM nodes ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Node
	for rows.Next() {
		n, err := scanNodeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// UpdateNode performs an optimistic-concurrency update. It increments version
// and fails with ErrConflict if the stored version doesn't match n.Version.
func (s *SQLiteStore) UpdateNode(n *model.Node) error {
	now := time.Now().UTC()
	labels, _ := json.Marshal(n.Labels)
	res, err := s.db.Exec(
		`UPDATE nodes SET type=?, title=?, body=?, labels=?, locked=?, version=version+1, updated_at=?
		 WHERE id=? AND version=?`,
		string(n.Type), n.Title, n.Body, string(labels), boolToInt(n.Locked),
		now.Format(time.RFC3339Nano),
		n.ID, n.Version,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrConflict
	}
	n.Version++
	n.UpdatedAt = now
	return nil
}

func (s *SQLiteStore) DeleteNode(id string) error {
	res, err := s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Node ↔ Chunk ---

func (s *SQLiteStore) GetNodeChunks(nodeID string) ([]model.Chunk, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.source_file, c.content, c.start_offset, c.end_offset, c.speaker, c.embedding_vec, c.created_at
		 FROM chunks c JOIN node_chunks nc ON c.id = nc.chunk_id
		 WHERE nc.node_id = ? ORDER BY nc.position`, nodeID)
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

func (s *SQLiteStore) SetNodeChunks(nodeID string, chunkIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM node_chunks WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	for i, cid := range chunkIDs {
		if _, err := tx.Exec(`INSERT INTO node_chunks (node_id, chunk_id, position) VALUES (?, ?, ?)`,
			nodeID, cid, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- Edges ---

func (s *SQLiteStore) CreateEdge(e *model.Edge) error {
	now := time.Now().UTC()
	e.CreatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO edges (id, from_node, to_node, type, condition, weight, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.FromNode, e.ToNode, string(e.Type), e.Condition, e.Weight,
		now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetEdge(id string) (*model.Edge, error) {
	row := s.db.QueryRow(
		`SELECT id, from_node, to_node, type, condition, weight, created_at FROM edges WHERE id = ?`, id)
	return scanEdge(row)
}

func (s *SQLiteStore) ListEdges() ([]model.Edge, error) {
	rows, err := s.db.Query(`SELECT id, from_node, to_node, type, condition, weight, created_at FROM edges ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Edge
	for rows.Next() {
		e, err := scanEdgeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListEdgesFrom(nodeID string) ([]model.Edge, error) {
	rows, err := s.db.Query(
		`SELECT id, from_node, to_node, type, condition, weight, created_at FROM edges WHERE from_node = ? ORDER BY weight`,
		nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Edge
	for rows.Next() {
		e, err := scanEdgeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListEdgesTo(nodeID string) ([]model.Edge, error) {
	rows, err := s.db.Query(
		`SELECT id, from_node, to_node, type, condition, weight, created_at FROM edges WHERE to_node = ? ORDER BY weight`,
		nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Edge
	for rows.Next() {
		e, err := scanEdgeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteEdge(id string) error {
	res, err := s.db.Exec(`DELETE FROM edges WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Threads ---

func (s *SQLiteStore) CreateThread(t *model.Thread) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO threads (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Description,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetThread(id string) (*model.Thread, error) {
	row := s.db.QueryRow(
		`SELECT id, name, description, created_at, updated_at FROM threads WHERE id = ?`, id)
	return scanThread(row)
}

func (s *SQLiteStore) ListThreads() ([]model.Thread, error) {
	rows, err := s.db.Query(`SELECT id, name, description, created_at, updated_at FROM threads ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Thread
	for rows.Next() {
		t, err := scanThreadRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateThread(t *model.Thread) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE threads SET name=?, description=?, updated_at=? WHERE id=?`,
		t.Name, t.Description, now.Format(time.RFC3339Nano), t.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	t.UpdatedAt = now
	return nil
}

func (s *SQLiteStore) DeleteThread(id string) error {
	res, err := s.db.Exec(`DELETE FROM threads WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Thread ↔ Node ordering ---

func (s *SQLiteStore) GetThreadNodes(threadID string) ([]model.ThreadNode, error) {
	rows, err := s.db.Query(
		`SELECT thread_id, node_id, position FROM thread_nodes WHERE thread_id = ? ORDER BY position`,
		threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ThreadNode
	for rows.Next() {
		var tn model.ThreadNode
		if err := rows.Scan(&tn.ThreadID, &tn.NodeID, &tn.Position); err != nil {
			return nil, err
		}
		out = append(out, tn)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SetThreadNodes(threadID string, nodes []model.ThreadNode) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM thread_nodes WHERE thread_id = ?`, threadID); err != nil {
		return err
	}
	// Sort by position before inserting.
	sorted := make([]model.ThreadNode, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Position < sorted[j].Position })

	for _, tn := range sorted {
		if _, err := tx.Exec(
			`INSERT INTO thread_nodes (thread_id, node_id, position) VALUES (?, ?, ?)`,
			threadID, tn.NodeID, tn.Position,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- scan helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanChunk(s scanner) (*model.Chunk, error) {
	var c model.Chunk
	var ts string
	if err := s.Scan(&c.ID, &c.SourceFile, &c.Content, &c.StartOffset, &c.EndOffset, &c.Speaker, &c.EmbeddingVec, &ts); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
	return &c, nil
}

func scanChunkRows(r *sql.Rows) (*model.Chunk, error) { return scanChunk(r) }

func scanNode(s scanner) (*model.Node, error) {
	var n model.Node
	var labelsJSON string
	var locked int
	var createdAt, updatedAt string
	if err := s.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &labelsJSON, &locked, &n.Version, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	json.Unmarshal([]byte(labelsJSON), &n.Labels)
	n.Locked = locked != 0
	n.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &n, nil
}

func scanNodeRows(r *sql.Rows) (*model.Node, error) { return scanNode(r) }

func scanEdge(s scanner) (*model.Edge, error) {
	var e model.Edge
	var ts string
	if err := s.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Type, &e.Condition, &e.Weight, &ts); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
	return &e, nil
}

func scanEdgeRows(r *sql.Rows) (*model.Edge, error) { return scanEdge(r) }

func scanThread(s scanner) (*model.Thread, error) {
	var t model.Thread
	var createdAt, updatedAt string
	if err := s.Scan(&t.ID, &t.Name, &t.Description, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &t, nil
}

func scanThreadRows(r *sql.Rows) (*model.Thread, error) { return scanThread(r) }

// --- Uploads ---

func (s *SQLiteStore) CreateUpload(u *model.Upload) error {
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

func (s *SQLiteStore) GetUpload(id string) (*model.Upload, error) {
	row := s.db.QueryRow(
		`SELECT id, filename, format, size_bytes, status, error, created_at, updated_at FROM uploads WHERE id = ?`, id)
	return scanUpload(row)
}

func (s *SQLiteStore) ListUploads() ([]model.Upload, error) {
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

func (s *SQLiteStore) UpdateUploadStatus(id string, status model.UploadStatus, errMsg string) error {
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

func (s *SQLiteStore) CreateTranscriptSegment(seg *model.TranscriptSegment) error {
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

func (s *SQLiteStore) ListTranscriptSegments(uploadID string) ([]model.TranscriptSegment, error) {
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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
