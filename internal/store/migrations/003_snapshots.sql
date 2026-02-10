-- Outline snapshots: full JSON copy of DAG state

CREATE TABLE IF NOT EXISTS snapshots (
    id         TEXT PRIMARY KEY,
    label      TEXT NOT NULL DEFAULT '',
    data       TEXT NOT NULL,  -- JSON blob: nodes, edges, threads, thread_nodes, node_chunks
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
