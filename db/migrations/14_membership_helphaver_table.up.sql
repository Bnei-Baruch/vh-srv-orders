BEGIN;

CREATE TABLE IF NOT EXISTS membership_helphaver (
    id                              SERIAL PRIMARY KEY,
    grant_id                        INT NOT NULL,
    membership_id                   INT NOT NULL,
    created_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                      TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT fk_grant_id          FOREIGN KEY(grant_id) REFERENCES "grant"(id),
    CONSTRAINT fk_membership_id     FOREIGN KEY(membership_id) REFERENCES membership(id)
);

COMMIT;