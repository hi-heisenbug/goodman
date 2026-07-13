ALTER TABLE alerts ADD COLUMN blocked BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE enforce_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    rev INTEGER NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL DEFAULT 0
);
INSERT INTO enforce_state (id, enabled, rev, updated_at) VALUES (1, FALSE, 0, 0);
