ALTER TABLE reachability_reports ADD COLUMN previous_report TEXT NOT NULL DEFAULT '';
ALTER TABLE reachability_reports ADD COLUMN previous_computed_at BIGINT NOT NULL DEFAULT 0;
