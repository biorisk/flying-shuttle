package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/google/uuid"
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
		"migrations/003_snapshots.sql",
		"migrations/004_branches.sql",
		"migrations/005_evidence.sql",
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

func (s *SQLiteStore) CreateChunks(chunks []model.Chunk) error {
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

// ListChunksBySourceFile returns every chunk from one source transcript,
// ordered by position within that transcript (start_offset).
func (s *SQLiteStore) ListChunksBySourceFile(sourceFile string) ([]model.Chunk, error) {
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
// total number of chunks in the store (so callers can render "N of M").
// A limit <= 0 means "no limit" (return everything from offset onward).
func (s *SQLiteStore) ListChunksPage(limit, offset int) ([]model.Chunk, int, error) {
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
// Used to reconcile the search index against the store on startup.
func (s *SQLiteStore) ListChunkIDs() ([]string, error) {
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
func (s *SQLiteStore) ListChunkIDsWithEmbedding() ([]string, error) {
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
func (s *SQLiteStore) GetChunksByIDs(ids []string) ([]model.Chunk, error) {
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
func (s *SQLiteStore) ListChunksMissingEmbedding(limit int) ([]model.Chunk, error) {
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
func (s *SQLiteStore) CountChunksMissingEmbedding() (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT count(*) FROM chunks WHERE embedding_vec IS NULL OR length(embedding_vec) = 0`,
	).Scan(&n)
	return n, err
}

// SetChunkEmbedding attaches (or replaces) a chunk's embedding vector. Chunk
// content is immutable; the embedding is derived metadata filled in
// asynchronously once an embedder is available.
func (s *SQLiteStore) SetChunkEmbedding(id string, vec []byte) error {
	res, err := s.db.Exec(`UPDATE chunks SET embedding_vec = ? WHERE id = ?`, vec, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chunk %s: %w", id, ErrNotFound)
	}
	return nil
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

// GetNodeChunks returns the distinct source chunks a node's evidence draws
// from, in evidence order. (node_chunks is superseded by the evidence table.)
func (s *SQLiteStore) GetNodeChunks(nodeID string) ([]model.Chunk, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.source_file, c.content, c.start_offset, c.end_offset, c.speaker, c.embedding_vec, c.created_at
		 FROM chunks c
		 JOIN (SELECT chunk_id, MIN(position) AS pos FROM evidence WHERE node_id = ? GROUP BY chunk_id) e
		   ON c.id = e.chunk_id
		 ORDER BY e.pos`, nodeID)
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

// SetNodeChunks replaces a node's evidence with whole-chunk spans for the
// given chunk IDs. Retained for the pre-evidence API surface (PUT
// /nodes/{id}/chunks); new code writes CreateEvidence directly.
func (s *SQLiteStore) SetNodeChunks(nodeID string, chunkIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM evidence WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	for i, cid := range chunkIDs {
		var sourceFile, content string
		if err := tx.QueryRow(`SELECT source_file, content FROM chunks WHERE id = ?`, cid).
			Scan(&sourceFile, &content); err != nil {
			return fmt.Errorf("chunk %s: %w", cid, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO evidence (`+evidenceCols+`)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
			uuid.NewString(), nodeID, cid, sourceFile, 0, len([]rune(content)), content, i,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListUsedChunkIDs() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT chunk_id FROM node_chunks
		UNION
		SELECT chunk_id FROM evidence`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- Evidence ---

func scanEvidenceRows(rows *sql.Rows) (*model.Evidence, error) {
	var e model.Evidence
	var createdAt string
	if err := rows.Scan(&e.ID, &e.NodeID, &e.ChunkID, &e.SourceFile,
		&e.CharStart, &e.CharEnd, &e.Text, &e.Position, &createdAt); err != nil {
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return &e, nil
}

const evidenceCols = `id, node_id, chunk_id, source_file, char_start, char_end, text, position, created_at`

func (s *SQLiteStore) CreateEvidence(e *model.Evidence) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	_, err := s.db.Exec(
		`INSERT INTO evidence (`+evidenceCols+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, COALESCE(NULLIF(?, ''), strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
		e.ID, e.NodeID, e.ChunkID, e.SourceFile, e.CharStart, e.CharEnd, e.Text, e.Position,
		formatTimeOrEmpty(e.CreatedAt))
	return err
}

func (s *SQLiteStore) ListNodeEvidence(nodeID string) ([]model.Evidence, error) {
	rows, err := s.db.Query(
		`SELECT `+evidenceCols+` FROM evidence WHERE node_id = ? ORDER BY position, created_at`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Evidence
	for rows.Next() {
		e, err := scanEvidenceRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ListAllEvidence returns every evidence row, ordered by node then position —
// used to render the whole outline and to serialize snapshots.
func (s *SQLiteStore) ListAllEvidence() ([]model.Evidence, error) {
	rows, err := s.db.Query(
		`SELECT ` + evidenceCols + ` FROM evidence ORDER BY node_id, position, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Evidence
	for rows.Next() {
		e, err := scanEvidenceRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteEvidence(id string) error {
	res, err := s.db.Exec(`DELETE FROM evidence WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteNodeEvidence(nodeID string) error {
	_, err := s.db.Exec(`DELETE FROM evidence WHERE node_id = ?`, nodeID)
	return err
}

// formatTimeOrEmpty renders t as RFC3339Nano, or "" for the zero value so the
// DB default fills in.
func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// --- Node Move ---

// MoveNode atomically moves a node to a new parent at a given sibling position.
// If newParentID is empty, the node becomes a root (no incoming linear edge).
func (s *SQLiteStore) MoveNode(nodeID, newParentID string, position int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete all incoming linear edges to this node.
	if _, err := tx.Exec(
		`DELETE FROM edges WHERE to_node = ? AND type = 'linear'`, nodeID,
	); err != nil {
		return err
	}

	// 2. If reparenting (not becoming root), create a new edge.
	if newParentID != "" {
		now := time.Now().UTC()
		newID := uuid.NewString()

		// Reweight existing children of the new parent: shift those at position
		// or later up by 1 to make room.
		if _, err := tx.Exec(
			`UPDATE edges SET weight = weight + 1
			 WHERE from_node = ? AND type = 'linear' AND weight >= ?`,
			newParentID, position,
		); err != nil {
			return err
		}

		if _, err := tx.Exec(
			`INSERT INTO edges (id, from_node, to_node, type, condition, weight, created_at)
			 VALUES (?, ?, ?, 'linear', NULL, ?, ?)`,
			newID, newParentID, nodeID, position,
			now.Format(time.RFC3339Nano),
		); err != nil {
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

// ListUploadsPage returns a page of uploads (newest first) plus the total
// upload count. A limit <= 0 means "no limit".
func (s *SQLiteStore) ListUploadsPage(limit, offset int) ([]model.Upload, int, error) {
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

// --- DAG state helpers (shared by snapshots and branches) ---

// ExportState returns the full DAG state (nodes, edges, evidence, threads).
func (s *SQLiteStore) ExportState() (*model.SnapshotData, error) {
	return s.gatherDAGState()
}

// ImportState replaces all live DAG tables with data, transactionally. Used
// for recovery when the database is lost but the working-doc state.json isn't.
func (s *SQLiteStore) ImportState(data *model.SnapshotData) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := clearDAGTables(tx); err != nil {
		return err
	}
	if err := restoreDAGState(tx, data); err != nil {
		return err
	}
	return tx.Commit()
}

// gatherDAGState collects the full DAG state from live tables.
func (s *SQLiteStore) gatherDAGState() (*model.SnapshotData, error) {
	nodes, err := s.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("gather nodes: %w", err)
	}
	edges, err := s.ListEdges()
	if err != nil {
		return nil, fmt.Errorf("gather edges: %w", err)
	}
	threads, err := s.ListThreads()
	if err != nil {
		return nil, fmt.Errorf("gather threads: %w", err)
	}

	var allThreadNodes []model.ThreadNode
	for _, t := range threads {
		tn, err := s.GetThreadNodes(t.ID)
		if err != nil {
			return nil, fmt.Errorf("gather thread_nodes %s: %w", t.ID, err)
		}
		allThreadNodes = append(allThreadNodes, tn...)
	}

	evidence, err := s.ListAllEvidence()
	if err != nil {
		return nil, fmt.Errorf("gather evidence: %w", err)
	}

	return &model.SnapshotData{
		Nodes:       nodes,
		Edges:       edges,
		Threads:     threads,
		ThreadNodes: allThreadNodes,
		Evidence:    evidence,
	}, nil
}

// clearDAGTables deletes all rows from the live DAG tables within a transaction.
func clearDAGTables(tx *sql.Tx) error {
	for _, table := range []string{"evidence", "node_chunks", "thread_nodes", "edges", "threads", "nodes"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}

// restoreDAGState inserts the full DAG state into live tables within a transaction.
func restoreDAGState(tx *sql.Tx, data *model.SnapshotData) error {
	for _, n := range data.Nodes {
		labels, _ := json.Marshal(n.Labels)
		if _, err := tx.Exec(
			`INSERT INTO nodes (id, type, title, body, labels, locked, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, string(n.Type), n.Title, n.Body, string(labels), boolToInt(n.Locked), n.Version,
			n.CreatedAt.Format(time.RFC3339Nano), n.UpdatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("restore node %s: %w", n.ID, err)
		}
	}
	for _, e := range data.Edges {
		if _, err := tx.Exec(
			`INSERT INTO edges (id, from_node, to_node, type, condition, weight, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.FromNode, e.ToNode, string(e.Type), e.Condition, e.Weight,
			e.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("restore edge %s: %w", e.ID, err)
		}
	}
	for _, t := range data.Threads {
		if _, err := tx.Exec(
			`INSERT INTO threads (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			t.ID, t.Name, t.Description,
			t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("restore thread %s: %w", t.ID, err)
		}
	}
	for _, tn := range data.ThreadNodes {
		if _, err := tx.Exec(
			`INSERT INTO thread_nodes (thread_id, node_id, position) VALUES (?, ?, ?)`,
			tn.ThreadID, tn.NodeID, tn.Position,
		); err != nil {
			return fmt.Errorf("restore thread_node: %w", err)
		}
	}
	for _, e := range data.Evidence {
		if _, err := tx.Exec(
			`INSERT INTO evidence (`+evidenceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			nz(e.ID), e.NodeID, e.ChunkID, e.SourceFile, e.CharStart, e.CharEnd, e.Text, e.Position,
			e.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("restore evidence: %w", err)
		}
	}
	// Legacy snapshots stored whole-chunk associations; carry them over as
	// full-span evidence rows.
	for _, nc := range data.NodeChunks {
		var content string
		if err := tx.QueryRow(`SELECT content FROM chunks WHERE id = ?`, nc.ChunkID).Scan(&content); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return fmt.Errorf("restore legacy node_chunk: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO evidence (`+evidenceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
			uuid.NewString(), nc.NodeID, nc.ChunkID, "", 0, len([]rune(content)), content, nc.Position,
		); err != nil {
			return fmt.Errorf("restore legacy node_chunk: %w", err)
		}
	}
	return nil
}

// nz returns id, or a fresh UUID when id is empty.
func nz(id string) string {
	if id == "" {
		return uuid.NewString()
	}
	return id
}

// --- Snapshots ---

func (s *SQLiteStore) CreateSnapshot(label string) (*model.SnapshotSummary, error) {
	data, err := s.gatherDAGState()
	if err != nil {
		return nil, err
	}
	blob, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}

	now := time.Now().UTC()
	id := uuid.NewString()
	if _, err := s.db.Exec(
		`INSERT INTO snapshots (id, label, data, created_at) VALUES (?, ?, ?, ?)`,
		id, label, string(blob), now.Format(time.RFC3339Nano),
	); err != nil {
		return nil, err
	}

	return &model.SnapshotSummary{ID: id, Label: label, CreatedAt: now}, nil
}

func (s *SQLiteStore) GetSnapshot(id string) (*model.Snapshot, error) {
	var snap model.Snapshot
	var dataBlob, ts string
	err := s.db.QueryRow(
		`SELECT id, label, data, created_at FROM snapshots WHERE id = ?`, id,
	).Scan(&snap.ID, &snap.Label, &dataBlob, &ts)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	snap.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
	if err := json.Unmarshal([]byte(dataBlob), &snap.Data); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &snap, nil
}

func (s *SQLiteStore) ListSnapshots() ([]model.SnapshotSummary, error) {
	rows, err := s.db.Query(`SELECT id, label, created_at FROM snapshots ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SnapshotSummary
	for rows.Next() {
		var ss model.SnapshotSummary
		var ts string
		if err := rows.Scan(&ss.ID, &ss.Label, &ts); err != nil {
			return nil, err
		}
		ss.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, ss)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteSnapshot(id string) error {
	res, err := s.db.Exec(`DELETE FROM snapshots WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) RestoreSnapshot(id string) error {
	snap, err := s.GetSnapshot(id)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := clearDAGTables(tx); err != nil {
		return err
	}
	if err := restoreDAGState(tx, &snap.Data); err != nil {
		return err
	}

	return tx.Commit()
}

// --- Branches ---

func (s *SQLiteStore) CreateBranch(name string) (*model.BranchSummary, error) {
	data, err := s.gatherDAGState()
	if err != nil {
		return nil, err
	}
	blob, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal branch data: %w", err)
	}
	dataStr := string(blob)

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Check if any branches exist already.
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM branches`).Scan(&count); err != nil {
		return nil, err
	}

	if count == 0 {
		// First split: create "main" branch (inactive) with current state.
		mainID := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO branches (id, name, data, active, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)`,
			mainID, "main", dataStr, nowStr, nowStr,
		); err != nil {
			return nil, err
		}
	} else {
		// Save current state into the departing (active) branch.
		if _, err := tx.Exec(
			`UPDATE branches SET data = ?, active = 0, updated_at = ? WHERE active = 1`,
			dataStr, nowStr,
		); err != nil {
			return nil, err
		}
	}

	// Create the new branch as active with the same state.
	newID := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO branches (id, name, data, active, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)`,
		newID, name, dataStr, nowStr, nowStr,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &model.BranchSummary{ID: newID, Name: name, Active: true, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *SQLiteStore) GetBranch(id string) (*model.Branch, error) {
	var b model.Branch
	var dataBlob, createdAt, updatedAt string
	var active int
	err := s.db.QueryRow(
		`SELECT id, name, data, active, created_at, updated_at FROM branches WHERE id = ?`, id,
	).Scan(&b.ID, &b.Name, &dataBlob, &active, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b.Active = active != 0
	b.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	b.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if err := json.Unmarshal([]byte(dataBlob), &b.Data); err != nil {
		return nil, fmt.Errorf("unmarshal branch data: %w", err)
	}
	return &b, nil
}

func (s *SQLiteStore) ListBranches() ([]model.BranchSummary, error) {
	rows, err := s.db.Query(`SELECT id, name, active, created_at, updated_at FROM branches ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BranchSummary
	for rows.Next() {
		var bs model.BranchSummary
		var active int
		var createdAt, updatedAt string
		if err := rows.Scan(&bs.ID, &bs.Name, &active, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		bs.Active = active != 0
		bs.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		bs.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		out = append(out, bs)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateBranch(id string, name string) (*model.BranchSummary, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE branches SET name = ?, updated_at = ? WHERE id = ?`,
		name, now.Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, ErrNotFound
	}
	// Re-read the branch summary.
	var bs model.BranchSummary
	var active int
	var createdAt, updatedAt string
	err = s.db.QueryRow(
		`SELECT id, name, active, created_at, updated_at FROM branches WHERE id = ?`, id,
	).Scan(&bs.ID, &bs.Name, &active, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	bs.Active = active != 0
	bs.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	bs.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &bs, nil
}

func (s *SQLiteStore) DeleteBranch(id string) error {
	// Check if the branch is active.
	var active int
	err := s.db.QueryRow(`SELECT active FROM branches WHERE id = ?`, id).Scan(&active)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if active != 0 {
		return ErrActiveBranch
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM branches WHERE id = ?`, id); err != nil {
		return err
	}

	// If only 1 branch remains, collapse to no-branch state.
	var remaining int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM branches`).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 1 {
		if _, err := tx.Exec(`DELETE FROM branches`); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) SwitchBranch(id string) error {
	// Gather current DAG state to save into the departing branch.
	data, err := s.gatherDAGState()
	if err != nil {
		return err
	}
	blob, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal departing branch: %w", err)
	}

	// Load target branch data.
	var targetBlob string
	var targetActive int
	err = s.db.QueryRow(`SELECT data, active FROM branches WHERE id = ?`, id).Scan(&targetBlob, &targetActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if targetActive != 0 {
		return nil // Already active, no-op.
	}
	var targetData model.SnapshotData
	if err := json.Unmarshal([]byte(targetBlob), &targetData); err != nil {
		return fmt.Errorf("unmarshal target branch: %w", err)
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Save current state into departing branch and mark inactive.
	if _, err := tx.Exec(
		`UPDATE branches SET data = ?, active = 0, updated_at = ? WHERE active = 1`,
		string(blob), nowStr,
	); err != nil {
		return err
	}

	// Clear live tables and restore target branch state.
	if err := clearDAGTables(tx); err != nil {
		return err
	}
	if err := restoreDAGState(tx, &targetData); err != nil {
		return err
	}

	// Mark target branch as active.
	if _, err := tx.Exec(
		`UPDATE branches SET active = 1, updated_at = ? WHERE id = ?`,
		nowStr, id,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetActiveBranch() (*model.BranchSummary, error) {
	var bs model.BranchSummary
	var active int
	var createdAt, updatedAt string
	err := s.db.QueryRow(
		`SELECT id, name, active, created_at, updated_at FROM branches WHERE active = 1`,
	).Scan(&bs.ID, &bs.Name, &active, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	bs.Active = true
	bs.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	bs.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &bs, nil
}
