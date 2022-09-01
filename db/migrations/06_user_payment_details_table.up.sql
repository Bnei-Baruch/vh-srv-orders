BEGIN;

CREATE TABLE IF NOT EXISTS payment_details (
    id                          SERIAL PRIMARY KEY,
    cc_number                   TEXT NOT NULL UNIQUE,
    cc_expdate                  TEXT NOT NULL UNIQUE,
    account_id                  INT NOT NULL UNIQUE,
    gateway_provider            TEXT NOT NULL UNIQUE,
    active                      BOOLEAN NOT NULL UNIQUE,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT fk_account_id    FOREIGN KEY(account_id) REFERENCES accounts(id)
);

COMMIT;