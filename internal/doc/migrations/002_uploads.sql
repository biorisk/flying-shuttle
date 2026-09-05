-- Audio uploads and transcript segments

CREATE TABLE IF NOT EXISTS uploads (
    id         TEXT PRIMARY KEY,
    filename   TEXT NOT NULL,
    format     TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'transcribing', 'done', 'failed')),
    error      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS transcript_segments (
    id        TEXT PRIMARY KEY,
    upload_id TEXT NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    speaker   TEXT NOT NULL DEFAULT '',
    text      TEXT NOT NULL,
    start_ms  INTEGER NOT NULL,
    end_ms    INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_segments_upload ON transcript_segments(upload_id);
CREATE INDEX IF NOT EXISTS idx_segments_time   ON transcript_segments(upload_id, start_ms);
