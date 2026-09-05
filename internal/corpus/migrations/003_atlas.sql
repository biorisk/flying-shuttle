-- Source Atlas: derived, disposable network over the corpus.
-- Concatenation of the historical atlas migrations 006/008/009/010.

-- ===== 006_atlas.sql =====
-- Source Atlas: a derived, disposable NETWORK over the transcript corpus.
--
-- Regions are clusters of embedding-near chunks; links connect similar regions
-- by centroid cosine. The Atlas is FLAT — no hierarchy, no parent/child, no
-- tiers, no containment. It is rebuilt from scratch (one build kept at a time),
-- and is never versioned, snapshotted, exported, or recovered.
--
-- This is NOT the authored outline. The outline has nodes and edges
-- (see 001_initial_schema.sql); the Atlas has regions and links. Do not
-- conflate the two: no code should join atlas_* to nodes/edges except through
-- chunks, and the only user-facing bridge is "add a member chunk as evidence"
-- via the existing evidence table.

CREATE TABLE IF NOT EXISTS atlas_build (
    id          TEXT PRIMARY KEY,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    status      TEXT NOT NULL DEFAULT 'building',   -- building | ready | failed
    chunk_count INTEGER NOT NULL DEFAULT 0,
    params_json TEXT NOT NULL DEFAULT '{}',
    error       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS atlas_region (
    id              TEXT PRIMARY KEY,
    build_id        TEXT NOT NULL REFERENCES atlas_build(id) ON DELETE CASCADE,
    centroid_vec    BLOB NOT NULL,
    chunk_count     INTEGER NOT NULL DEFAULT 0,
    digest_title    TEXT NOT NULL DEFAULT '',
    digest_abstract TEXT NOT NULL DEFAULT '',
    digest_keywords TEXT NOT NULL DEFAULT '',       -- newline-joined
    digest_source   TEXT NOT NULL DEFAULT '',       -- "llm:<model>" | "extractive"
    digest_vec      BLOB                            -- nullable until Phase C
);
CREATE INDEX IF NOT EXISTS idx_atlas_region_build ON atlas_region(build_id);

-- Undirected: each pair is stored once with region_a_id < region_b_id.
CREATE TABLE IF NOT EXISTS atlas_region_link (
    build_id    TEXT NOT NULL REFERENCES atlas_build(id)  ON DELETE CASCADE,
    region_a_id TEXT NOT NULL REFERENCES atlas_region(id) ON DELETE CASCADE,
    region_b_id TEXT NOT NULL REFERENCES atlas_region(id) ON DELETE CASCADE,
    weight      REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (region_a_id, region_b_id),
    CHECK (region_a_id < region_b_id)
);
CREATE INDEX IF NOT EXISTS idx_atlas_link_build ON atlas_region_link(build_id);

-- Each chunk is a member of exactly one region within a build.
CREATE TABLE IF NOT EXISTS atlas_region_chunk (
    region_id TEXT NOT NULL REFERENCES atlas_region(id) ON DELETE CASCADE,
    chunk_id  TEXT NOT NULL REFERENCES chunks(id)       ON DELETE CASCADE,
    distance  REAL NOT NULL DEFAULT 0,                  -- cosine distance to centroid
    keywords  TEXT NOT NULL DEFAULT '',                 -- newline-joined
    PRIMARY KEY (region_id, chunk_id)
);
CREATE INDEX IF NOT EXISTS idx_atlas_region_chunk_chunk ON atlas_region_chunk(chunk_id);

-- ===== 008_atlas_transcript.sql =====
-- Source Atlas: one digest per transcript (source file) in a build.
--
-- These label the network graph's top-level transcript nodes. Unlike a
-- region digest (built from a cluster of embedding-near chunks that may span
-- many files), a transcript digest is built from that single file's own full
-- text, in document order. Same disposable-rebuild lifecycle as the rest of
-- the atlas_* tables: replaced wholesale on every build, cascade-deleted with
-- the build.

CREATE TABLE IF NOT EXISTS atlas_transcript (
    build_id        TEXT NOT NULL REFERENCES atlas_build(id) ON DELETE CASCADE,
    source_file     TEXT NOT NULL,
    chunk_count     INTEGER NOT NULL DEFAULT 0,
    digest_title    TEXT NOT NULL DEFAULT '',
    digest_abstract TEXT NOT NULL DEFAULT '',
    digest_keywords TEXT NOT NULL DEFAULT '',       -- newline-joined
    digest_source   TEXT NOT NULL DEFAULT '',       -- "llm:<model>" | "extractive"
    PRIMARY KEY (build_id, source_file)
);
CREATE INDEX IF NOT EXISTS idx_atlas_transcript_build ON atlas_transcript(build_id);

-- ===== 009_atlas_chunk_label.sql =====
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

-- ===== 010_atlas_digest.sql =====
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

