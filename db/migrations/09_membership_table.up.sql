BEGIN;

CREATE TABLE IF NOT EXISTS membership (
    id                          SERIAL PRIMARY KEY,
    active                      BOOLEAN NOT NULL,
    type                        TEXT NOT NULL,
    month                       INT NOT NULL,
    year                        INT NOT NULL,
    expire                      TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now()
);

COMMIT;