BEGIN;

CREATE TABLE IF NOT EXISTS operation_trace (
    id SERIAL PRIMARY KEY,
    input JSON NOT NULL,
    type TEXT NOT NULL,
    output JSON NOT NULL,
    status TEXT NOT NULL,
    revert JSON
);

COMMIT;