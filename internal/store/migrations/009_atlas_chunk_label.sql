-- Source Atlas: a short LLM-written label per chunk, for the transcript
-- drill-down view's node labels.
--
-- Keyed by the immutable chunk, NOT by a build: unlike every other atlas_*
-- table this one does NOT cascade from atlas_build, so a rebuild never
-- recomputes an "llm:<model>" label that already exists. Adding a new
-- transcript and rebuilding labels only its new chunks. A row dies only when
-- its chunk does.
--
-- source = "head" marks a best-effort fallback written when the LLM was down
-- or mangled that line; the next build re-attempts those rows (only real
-- "llm:<model>" rows are treated as done).

CREATE TABLE IF NOT EXISTS atlas_chunk_label (
    chunk_id   TEXT PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    label      TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT '',   -- "llm:<model>" (final) | "head" (retried)
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
