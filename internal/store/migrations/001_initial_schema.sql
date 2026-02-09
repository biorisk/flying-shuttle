-- Flying Shuttle: initial schema
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

CREATE TABLE IF NOT EXISTS nodes (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('outline', 'chunk_ref', 'synth')),
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    labels     TEXT NOT NULL DEFAULT '{}',
    locked     INTEGER NOT NULL DEFAULT 0,
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS node_chunks (
    node_id  TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    chunk_id TEXT NOT NULL REFERENCES chunks(id) ON DELETE RESTRICT,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, chunk_id)
);

CREATE TABLE IF NOT EXISTS edges (
    id        TEXT PRIMARY KEY,
    from_node TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_node   TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    type      TEXT NOT NULL CHECK (type IN ('linear', 'branch', 'jump')),
    condition TEXT,
    weight    INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (from_node, to_node)
);

CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_node);
CREATE INDEX IF NOT EXISTS idx_edges_to   ON edges(to_node);

CREATE TABLE IF NOT EXISTS threads (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS thread_nodes (
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    node_id   TEXT NOT NULL REFERENCES nodes(id)   ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    PRIMARY KEY (thread_id, node_id),
    UNIQUE (thread_id, position)
);
