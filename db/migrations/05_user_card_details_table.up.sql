BEGIN;

CREATE TABLE IF NOT EXISTS card_details (
    id                          SERIAL PRIMARY KEY,
    cc_number                   TEXT NOT NULL,
    cc_expdate                  TEXT NOT NULL,
    account_id                  INT NOT NULL,
    gateway_provider            TEXT NOT NULL,
    active                      BOOLEAN NOT NULL,
    token                       TEXT NOT NULL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT fk_account_id    FOREIGN KEY(account_id) REFERENCES accounts(id),
    CONSTRAINT uq_cc_number_account_id_gateway_key UNIQUE (cc_number, account_id, gateway_provider)
);

COMMIT;