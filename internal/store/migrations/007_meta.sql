-- Small key/value table for cross-run bookkeeping that isn't a domain entity.
-- First use: recording which embedding model produced chunks.embedding_vec, so
-- a model/dimension change can invalidate the stale vectors on the next boot.

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
