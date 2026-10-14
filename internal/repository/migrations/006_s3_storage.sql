CREATE TABLE IF NOT EXISTS storage_config (
    id TEXT PRIMARY KEY CHECK (id = 'default'),
    enabled INTEGER NOT NULL DEFAULT 0,
    bucket TEXT NOT NULL DEFAULT '',
    region TEXT NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    path_style INTEGER NOT NULL DEFAULT 0,
    key_prefix TEXT NOT NULL DEFAULT '',
    credentials BLOB NOT NULL DEFAULT X'',
    masked_access_key_id TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS document_version_storage (
    version_id TEXT PRIMARY KEY REFERENCES document_versions(id) ON DELETE CASCADE,
    bucket TEXT NOT NULL CHECK (length(bucket) > 0),
    object_key TEXT NOT NULL CHECK (length(object_key) > 0)
);
CREATE TABLE IF NOT EXISTS storage_cleanup_jobs (
    id INTEGER PRIMARY KEY,
    bucket TEXT NOT NULL,
    object_key TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt INTEGER NOT NULL DEFAULT 0,
    UNIQUE(bucket, object_key)
);
CREATE INDEX IF NOT EXISTS idx_storage_cleanup_due ON storage_cleanup_jobs(next_attempt);
