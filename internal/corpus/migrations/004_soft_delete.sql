-- Corpus chunks are append-only: a re-ingested transcript creates NEW chunk
-- rows (new ids) and soft-deletes the superseded ones rather than removing
-- them, so evidence rows in every bound project keep resolving. Normal reads
-- filter deleted_at IS NULL; the atlas ignores soft-deleted chunks on its
-- next build; `shuttle doctor` reports evidence that cites a superseded chunk.
ALTER TABLE chunks ADD COLUMN deleted_at TEXT;
CREATE INDEX IF NOT EXISTS idx_chunks_live ON chunks(deleted_at) WHERE deleted_at IS NULL;
