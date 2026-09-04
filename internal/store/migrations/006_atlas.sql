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
