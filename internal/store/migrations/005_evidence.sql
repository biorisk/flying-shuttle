-- Evidence: a node's supporting passage. Unlike node_chunks (whole-chunk
-- association), an evidence row records an arbitrary text span within a chunk
-- (char_start..char_end are rune offsets into chunks.content; a whole-chunk
-- attachment has char_start = 0 and char_end = length(content)). The excerpt
-- text is stored verbatim so stitch/export never has to re-resolve it.

CREATE TABLE IF NOT EXISTS evidence (
    id          TEXT PRIMARY KEY,
    node_id     TEXT NOT NULL REFERENCES nodes(id)  ON DELETE CASCADE,
    chunk_id    TEXT NOT NULL REFERENCES chunks(id) ON DELETE RESTRICT,
    source_file TEXT NOT NULL DEFAULT '',
    char_start  INTEGER NOT NULL DEFAULT 0,
    char_end    INTEGER NOT NULL DEFAULT 0,
    text        TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_evidence_node  ON evidence(node_id);
CREATE INDEX IF NOT EXISTS idx_evidence_chunk ON evidence(chunk_id);

-- One-time backfill: carry existing whole-chunk associations over as evidence
-- spanning the full chunk. Guarded so re-running the migration is a no-op.
INSERT INTO evidence (id, node_id, chunk_id, source_file, char_start, char_end, text, position, created_at)
SELECT lower(hex(randomblob(16))),
       nc.node_id, nc.chunk_id, c.source_file,
       0, length(c.content), c.content, nc.position, c.created_at
FROM node_chunks nc
JOIN chunks c ON c.id = nc.chunk_id
WHERE NOT EXISTS (
    SELECT 1 FROM evidence e WHERE e.node_id = nc.node_id AND e.chunk_id = nc.chunk_id
);
