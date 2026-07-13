CREATE TABLE IF NOT EXISTS lockfiles (
    service      TEXT PRIMARY KEY,       -- '' is the all-services scope
    content      TEXT NOT NULL,          -- raw package-lock.json
    uploaded_at  BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS reachability_reports (
    service      TEXT PRIMARY KEY,
    report       JSONB NOT NULL DEFAULT '{}'::jsonb,  -- marshaled report.Report
    osv          BOOLEAN NOT NULL DEFAULT FALSE,
    computed_at  BIGINT NOT NULL
);
