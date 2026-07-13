CREATE TABLE IF NOT EXISTS lockfiles (
    service      TEXT PRIMARY KEY,       -- '' is the all-services scope
    content      TEXT NOT NULL,          -- raw package-lock.json
    uploaded_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS reachability_reports (
    service      TEXT PRIMARY KEY,
    report       TEXT NOT NULL DEFAULT '{}',  -- marshaled report.Report
    osv          INTEGER NOT NULL DEFAULT 0,
    computed_at  INTEGER NOT NULL
);
