BEGIN;

CREATE TABLE IF NOT EXISTS membership_special (
    id                              SERIAL PRIMARY KEY,
    membership_id                   INT NOT NULL,
    created_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    CONSTRAINT fk_membership_id     FOREIGN KEY(membership_id) REFERENCES membership(id)
);

COMMIT;