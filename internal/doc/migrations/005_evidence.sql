-- Evidence: a node's supporting passage. An evidence row records an arbitrary
-- text span within a source chunk (char_start..char_end are rune offsets into
-- the chunk content; a whole-chunk attachment has char_start = 0 and
-- char_end = length(content)). The excerpt text, source_file and offsets are
-- stored verbatim so the outline renders and exports without the corpus.
--
-- chunk_id references a row in the corpus database and therefore carries NO
-- SQL foreign key (SQLite cannot enforce one across databases). CreateEvidence
-- validates it against the bound corpus; `shuttle doctor` reports danglers.
CREATE TABLE IF NOT EXISTS evidence (
    id          TEXT PRIMARY KEY,
    node_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    chunk_id    TEXT NOT NULL,
    source_file TEXT NOT NULL DEFAULT '',
    char_start  INTEGER NOT NULL DEFAULT 0,
    char_end    INTEGER NOT NULL DEFAULT 0,
    text        TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_evidence_node  ON evidence(node_id);
CREATE INDEX IF NOT EXISTS idx_evidence_chunk ON evidence(chunk_id);
