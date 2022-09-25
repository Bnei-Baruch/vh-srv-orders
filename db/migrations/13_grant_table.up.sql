BEGIN;

CREATE TABLE IF NOT EXISTS "grant" (
    id                              SERIAL PRIMARY KEY,
    amount                          INT NOT NULL,
    currency                        TEXT NOT NULL,
    loaned                          INT NOT NULL,
    granted                         INT NOT NULL,
    repayed                         INT NOT NULL,
    created_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                      TIMESTAMP WITH TIME ZONE DEFAULT null
);

COMMIT;