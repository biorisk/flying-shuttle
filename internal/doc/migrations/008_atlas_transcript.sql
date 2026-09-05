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
