CREATE TABLE IF NOT EXISTS fingerprints (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    service      TEXT NOT NULL,
    package      TEXT NOT NULL,
    version      TEXT NOT NULL,
    behaviors    TEXT NOT NULL DEFAULT '{}',
    first_seen   INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    obs_count    INTEGER NOT NULL DEFAULT 0,
    is_baseline  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (service, package, version)
);
CREATE INDEX IF NOT EXISTS idx_fp_pkg ON fingerprints (package, version);

CREATE TABLE IF NOT EXISTS alerts (
    id            TEXT PRIMARY KEY,
    service       TEXT NOT NULL,
    package       TEXT NOT NULL,
    old_version   TEXT,
    new_version   TEXT NOT NULL,
    severity      TEXT NOT NULL,
    new_behaviors TEXT NOT NULL,
    detected_at   INTEGER NOT NULL,
    status        TEXT NOT NULL DEFAULT 'open'
);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts (status, detected_at DESC);
