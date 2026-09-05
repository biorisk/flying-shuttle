package main

import (
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/biorisk/flying-shuttle/internal/project"
	_ "modernc.org/sqlite"
)

// docTables live in project.db after the split; corpusTables live in corpus.db.
var (
	docTables = []string{
		"nodes", "edges", "threads", "thread_nodes",
		"evidence", "snapshots", "branches",
	}
	corpusTables = []string{
		"chunks", "meta", "uploads", "transcript_segments",
		"atlas_build", "atlas_region", "atlas_region_link", "atlas_region_chunk",
		"atlas_transcript", "atlas_chunk_label", "atlas_digest",
	}
	// legacyTables exist only in old databases and are dropped from both halves.
	legacyTables = []string{"node_chunks"}
)

func runMigrate(args []string) error {
	if len(args) == 0 || args[0] != "split" {
		return fmt.Errorf("usage: shuttle migrate split")
	}
	home, err := project.Home()
	if err != nil {
		return err
	}
	return migrateSplit(home)
}

// migrateSplit converts every pre-split ~/.shuttle/<name>/ project directory
// into the projects/<name>/ + corpora/<name>/ layout. The whole ~/.shuttle
// tree is copied to a timestamped backup first; each converted directory is
// left in place renamed to <name>.pre-split/.
func migrateSplit(home string) error {
	fi, err := os.Stat(home)
	if err != nil {
		return fmt.Errorf("no shuttle home at %s: %w", home, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", home)
	}

	olds, err := preSplitProjects(home)
	if err != nil {
		return err
	}
	if len(olds) == 0 {
		return fmt.Errorf("nothing to migrate: no pre-split project dirs under %s", home)
	}

	// Refuse if a split layout already has content — re-running would clobber.
	for _, dir := range []string{project.ProjectsDir(home), project.CorporaDir(home)} {
		if entries, _ := os.ReadDir(dir); len(entries) > 0 {
			return fmt.Errorf("%s already exists and is non-empty; migration already run?", dir)
		}
	}

	backup := filepath.Join(os.TempDir(), fmt.Sprintf("shuttle-backup-%s", time.Now().Format("20060102-150405")))
	log.Printf("backing up %s -> %s", home, backup)
	if err := copyTree(home, backup); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	for _, name := range olds {
		if err := splitOne(home, name); err != nil {
			return fmt.Errorf("split %q: %w  (backup at %s)", name, err, backup)
		}
		log.Printf("migrated %q", name)
	}
	log.Printf("done: %d project(s) migrated. Backup: %s", len(olds), backup)
	log.Printf("original dirs kept as <name>.pre-split/ — remove them once you've confirmed.")
	return nil
}

// preSplitProjects lists ~/.shuttle subdirectories that look like an old-layout
// project (a valid name, holding shuttle.db), excluding the new trees.
func preSplitProjects(home string) ([]string, error) {
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !project.ValidName(e.Name()) {
			continue
		}
		if e.Name() == "projects" || e.Name() == "corpora" {
			continue
		}
		if _, err := os.Stat(filepath.Join(home, e.Name(), "shuttle.db")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func splitOne(home, name string) error {
	old := filepath.Join(home, name)
	pp, err := project.CreateProject(home, name, "")
	if err != nil {
		return err
	}
	cp, err := project.CreateCorpus(home, name)
	if err != nil {
		return err
	}

	// 1. corpus.db = shuttle.db minus the document tables.
	if err := copyFile(filepath.Join(old, "shuttle.db"), cp.DB); err != nil {
		return err
	}
	if err := dropTables(cp.DB, append(docTables, legacyTables...)); err != nil {
		return err
	}

	// 2. project.db = shuttle.db minus the corpus tables, evidence FK removed.
	if err := copyFile(filepath.Join(old, "shuttle.db"), pp.DB); err != nil {
		return err
	}
	if err := dropTables(pp.DB, append(corpusTables, legacyTables...)); err != nil {
		return err
	}
	if err := rebuildEvidenceWithoutFK(pp.DB); err != nil {
		return err
	}

	// 3. move index snapshots + uploads to the corpus dir.
	moveIfExists(filepath.Join(old, "shuttle.bm25"), cp.BM25)
	moveIfExists(filepath.Join(old, "shuttle.hnsw"), cp.HNSW)
	if _, err := os.Stat(filepath.Join(old, "uploads")); err == nil {
		os.RemoveAll(cp.UploadDir)
		if err := os.Rename(filepath.Join(old, "uploads"), cp.UploadDir); err != nil {
			return err
		}
	}

	// 4. move the working-doc mirror to the project dir.
	moveIfExists(filepath.Join(old, "outline.md"), pp.OutlineMD)
	moveIfExists(filepath.Join(old, "state.json"), pp.StateJSON)
	if _, err := os.Stat(filepath.Join(old, "branches")); err == nil {
		os.RemoveAll(pp.BranchDir)
		if err := os.Rename(filepath.Join(old, "branches"), pp.BranchDir); err != nil {
			return err
		}
	}

	// 5. write the binding, then retire the original directory.
	if err := project.WriteBinding(pp, name); err != nil {
		return err
	}
	return os.Rename(old, old+".pre-split")
}

func dropTables(dbPath string, tables []string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
			return fmt.Errorf("drop %s: %w", t, err)
		}
	}
	_, err = db.Exec("VACUUM")
	return err
}

// rebuildEvidenceWithoutFK recreates the evidence table without the
// REFERENCES chunks(id) foreign key (chunks now lives in a separate database).
func rebuildEvidenceWithoutFK(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var ddl string
	err = db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='evidence'`).Scan(&ddl)
	if err == sql.ErrNoRows {
		return nil // no evidence table (fresh/empty project)
	}
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		"PRAGMA foreign_keys = OFF",
		"ALTER TABLE evidence RENAME TO evidence__old",
		`CREATE TABLE evidence (
			id          TEXT PRIMARY KEY,
			node_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			chunk_id    TEXT NOT NULL,
			source_file TEXT NOT NULL DEFAULT '',
			char_start  INTEGER NOT NULL DEFAULT 0,
			char_end    INTEGER NOT NULL DEFAULT 0,
			text        TEXT NOT NULL DEFAULT '',
			position    INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`,
		`INSERT INTO evidence (id, node_id, chunk_id, source_file, char_start, char_end, text, position, created_at)
		 SELECT id, node_id, chunk_id, COALESCE(source_file,''), COALESCE(char_start,0), COALESCE(char_end,0),
		        COALESCE(text,''), COALESCE(position,0),
		        COALESCE(created_at, strftime('%Y-%m-%dT%H:%M:%fZ','now')) FROM evidence__old`,
		"DROP TABLE evidence__old",
		"CREATE INDEX IF NOT EXISTS idx_evidence_node  ON evidence(node_id)",
		"CREATE INDEX IF NOT EXISTS idx_evidence_chunk ON evidence(chunk_id)",
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return tx.Commit()
}

// --- file helpers ---

func moveIfExists(src, dst string) {
	if _, err := os.Stat(src); err != nil {
		return
	}
	if err := os.Rename(src, dst); err != nil {
		log.Printf("  move %s -> %s: %v", src, dst, err)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}
