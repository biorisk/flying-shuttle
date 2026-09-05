-- Corpus schema: the shared, build-once half of a workspace. Chunks (with
-- their embedding vectors) and the meta key/value table. Uploads and
-- transcript segments are in 002; the atlas is in 003.
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS chunks (
    id          TEXT PRIMARY KEY,
    source_file TEXT NOT NULL,
    content     TEXT NOT NULL,
    start_offset INTEGER NOT NULL,
    end_offset   INTEGER NOT NULL,
    speaker     TEXT,
    embedding_vec BLOB,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Small key/value table for cross-run bookkeeping (e.g. which embedding model
-- produced chunks.embedding_vec, so a model change invalidates stale vectors).
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
