CREATE TABLE IF NOT EXISTS fingerprints (
    id           BIGSERIAL PRIMARY KEY,
    service      TEXT NOT NULL,
    package      TEXT NOT NULL,
    version      TEXT NOT NULL,
    behaviors    JSONB NOT NULL DEFAULT '{}',  -- { "READ /app/**": {"count":N,"first":ts,"last":ts}, ... }
    first_seen   BIGINT NOT NULL,
    last_seen    BIGINT NOT NULL,
    obs_count    INT NOT NULL DEFAULT 0,
    is_baseline  BOOLEAN NOT NULL DEFAULT FALSE, -- promoted after the learning window
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
    new_behaviors JSONB NOT NULL,
    detected_at   BIGINT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'open'   -- open | acknowledged | resolved
);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts (status, detected_at DESC);
