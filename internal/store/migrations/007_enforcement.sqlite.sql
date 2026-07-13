ALTER TABLE alerts ADD COLUMN blocked BOOLEAN NOT NULL DEFAULT 0;

CREATE TABLE enforce_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 0,
    rev INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);
INSERT INTO enforce_state (id, enabled, rev, updated_at) VALUES (1, 0, 0, 0);
