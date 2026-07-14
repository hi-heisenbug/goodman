CREATE TABLE IF NOT EXISTS reachability_report_history (
    service     TEXT NOT NULL,
    report      TEXT NOT NULL,
    osv         INTEGER NOT NULL DEFAULT 0,
    computed_at INTEGER NOT NULL,
    PRIMARY KEY (service, computed_at)
);
CREATE INDEX IF NOT EXISTS idx_reachability_history_lookup
    ON reachability_report_history (service, computed_at DESC);
