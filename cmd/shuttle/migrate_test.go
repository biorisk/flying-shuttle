package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/project"
	_ "modernc.org/sqlite"
)

// seedPreSplitDB writes a shuttle.db with both halves populated and the old
// evidence.chunk_id foreign key, mimicking a real pre-split project.
func seedPreSplitDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, source_file TEXT, content TEXT, start_offset INT, end_offset INT, speaker TEXT, embedding_vec BLOB, created_at TEXT)`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE uploads (id TEXT PRIMARY KEY, filename TEXT, format TEXT, size_bytes INT, status TEXT, error TEXT, created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE transcript_segments (id TEXT PRIMARY KEY, upload_id TEXT, speaker TEXT, text TEXT, start_ms INT, end_ms INT, created_at TEXT)`,
		`CREATE TABLE atlas_build (id TEXT PRIMARY KEY, created_at TEXT, status TEXT, chunk_count INT, params_json TEXT, error TEXT)`,
		`CREATE TABLE nodes (id TEXT PRIMARY KEY, type TEXT, title TEXT, body TEXT, labels TEXT, locked INT, version INT, created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE edges (id TEXT PRIMARY KEY, from_node TEXT, to_node TEXT, type TEXT, condition TEXT, weight INT, created_at TEXT)`,
		`CREATE TABLE threads (id TEXT PRIMARY KEY, name TEXT, description TEXT, created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE thread_nodes (thread_id TEXT, node_id TEXT, position INT)`,
		`CREATE TABLE snapshots (id TEXT PRIMARY KEY, label TEXT, data TEXT, created_at TEXT)`,
		`CREATE TABLE branches (id TEXT PRIMARY KEY, name TEXT, base_data TEXT, active INT, created_at TEXT)`,
		`CREATE TABLE evidence (id TEXT PRIMARY KEY, node_id TEXT REFERENCES nodes(id), chunk_id TEXT NOT NULL REFERENCES chunks(id) ON DELETE RESTRICT, source_file TEXT, char_start INT, char_end INT, text TEXT, position INT, created_at TEXT)`,
		`INSERT INTO chunks (id, source_file, content, start_offset, end_offset, created_at) VALUES ('c1', 'iv.txt', 'hello world', 0, 11, '2026-01-01T00:00:00Z')`,
		`INSERT INTO meta (key, value) VALUES ('embed_model', 'gemma-768')`,
		`INSERT INTO nodes (id, type, title, body, labels, locked, version, created_at, updated_at) VALUES ('n1', 'outline', 'Root', '', '{}', 0, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO evidence (id, node_id, chunk_id, source_file, char_start, char_end, text, position, created_at) VALUES ('e1', 'n1', 'c1', 'iv.txt', 0, 11, 'hello world', 0, '2026-01-01T00:00:00Z')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

func TestMigrateSplit(t *testing.T) {
	home := t.TempDir()

	old := filepath.Join(home, "default")
	if err := os.MkdirAll(filepath.Join(old, "branches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(old, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedPreSplitDB(t, filepath.Join(old, "shuttle.db"))
	os.WriteFile(filepath.Join(old, "outline.md"), []byte("# book\n"), 0o644)
	os.WriteFile(filepath.Join(old, "state.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(old, "shuttle.bm25"), []byte("bm25"), 0o644)
	os.WriteFile(filepath.Join(old, "uploads", "iv.txt"), []byte("hello world"), 0o644)

	if err := migrateSplit(home); err != nil {
		t.Fatal(err)
	}

	// Old dir retired.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("original dir should be gone, got err=%v", err)
	}
	if _, err := os.Stat(old + ".pre-split"); err != nil {
		t.Fatalf(".pre-split backup missing: %v", err)
	}

	pp := project.ProjectPathsFor(home, "default")
	cp := project.CorpusPathsFor(home, "default")

	// Files moved.
	for _, f := range []string{pp.OutlineMD, pp.StateJSON, pp.DB, pp.JSON, cp.DB, cp.BM25, filepath.Join(cp.UploadDir, "iv.txt")} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("expected %s: %v", f, err)
		}
	}

	// Binding written.
	if name, _ := project.ReadBinding(pp); name != "default" {
		t.Fatalf("binding = %q", name)
	}

	// project.db opens under the doc store; corpus tables gone, data intact.
	d, err := doc.Open(pp.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	nodes, _ := d.ListNodes()
	if len(nodes) != 1 || nodes[0].ID != "n1" {
		t.Fatalf("nodes not preserved: %+v", nodes)
	}
	ev, _ := d.ListNodeEvidence("n1")
	if len(ev) != 1 || ev[0].ChunkID != "c1" {
		t.Fatalf("evidence not preserved: %+v", ev)
	}

	// Evidence FK is gone: an evidence row citing a nonexistent chunk inserts.
	if _, err := d.(interface{ DB() *sql.DB }).DB().Exec(
		`INSERT INTO evidence (id, node_id, chunk_id, char_end) VALUES ('e2', 'n1', 'ghost', 0)`); err != nil {
		t.Fatalf("evidence should have no chunk FK: %v", err)
	}
	if _, err := d.(interface{ DB() *sql.DB }).DB().Exec(`SELECT 1 FROM chunks`); err == nil {
		t.Fatal("project.db should not have a chunks table")
	}

	// corpus.db opens under the corpus store; doc tables gone, data intact.
	c, err := corpus.Open(cp.DB, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := c.GetChunk("c1")
	if err != nil || got.Content != "hello world" {
		t.Fatalf("chunk not preserved: %+v err=%v", got, err)
	}
	if v, _ := c.GetMeta("embed_model"); v != "gemma-768" {
		t.Fatalf("meta not preserved: %q", v)
	}
	if _, err := c.DB().Exec(`SELECT 1 FROM nodes`); err == nil {
		t.Fatal("corpus.db should not have a nodes table")
	}

	// Re-running refuses.
	if err := migrateSplit(home); err == nil {
		t.Fatal("second migrate should fail")
	}
}
