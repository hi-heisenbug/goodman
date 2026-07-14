CREATE TABLE IF NOT EXISTS reachability_report_history (
    service     TEXT NOT NULL,
    report      JSONB NOT NULL,
    osv         BOOLEAN NOT NULL DEFAULT FALSE,
    computed_at BIGINT NOT NULL,
    PRIMARY KEY (service, computed_at)
);
CREATE INDEX IF NOT EXISTS idx_reachability_history_lookup
    ON reachability_report_history (service, computed_at DESC);
