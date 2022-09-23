BEGIN;

CREATE TABLE IF NOT EXISTS terminal (
    id                          TEXT PRIMARY KEY,
    key                         TEXT NOT NULL,
    gateway                     TEXT NOT NULL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now()
);

COMMIT;