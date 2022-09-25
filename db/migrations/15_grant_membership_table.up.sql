BEGIN;

CREATE TABLE IF NOT EXISTS grant_membership (
    id                              SERIAL PRIMARY KEY,
    grant_id                        INT NOT NULL,
    nb_month                        INT NOT NULL,
    month_used                      INT NOT NULL,
    month_left                      INT NOT NULL,
    created_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                      TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT fk_grant_id          FOREIGN KEY(grant_id) REFERENCES "grant"(id)
);

COMMIT;