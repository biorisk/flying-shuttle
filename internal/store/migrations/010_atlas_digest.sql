-- Source Atlas: a content-addressed cache of LLM digests (region + transcript).
--
-- Keyed by a hash of the summariser input (the immutable set of chunk ids
-- that went in), NOT by a build. Same lifecycle as atlas_chunk_label: it does
-- NOT cascade from atlas_build, so an unchanged cluster / transcript reuses
-- its digest across every rebuild, and a build that crashes mid-Phase-B loses
-- at most the one in-flight LLM call.
--
-- source = "llm:<model>" is final and always reused. source = "extractive" is
-- provisional (LLM was down): reused only when there's still no LLM, else
-- recomputed and upserted.

CREATE TABLE IF NOT EXISTS atlas_digest (
    input_hash TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,              -- 'region' | 'transcript'
    title      TEXT NOT NULL DEFAULT '',
    abstract   TEXT NOT NULL DEFAULT '',
    keywords   TEXT NOT NULL DEFAULT '',   -- newline-joined
    vec        BLOB,                       -- digest embedding; NULL until embedded
    source     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_atlas_digest_kind ON atlas_digest(kind);
