BEGIN;

CREATE TABLE IF NOT EXISTS payments_offline (
    id                      SERIAL PRIMARY KEY,
    payment_method          TEXT NOT NULL,
    receipt                 TEXT,
    extra_info              TEXT,
    status                  TEXT,
    payment_id              INT NOT NULL,
    created_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    CONSTRAINT fk_payment_id  FOREIGN KEY(payment_id) REFERENCES payments(id)
);

CREATE TABLE IF NOT EXISTS payments_helphaver (
    id                      SERIAL PRIMARY KEY,
    status                  TEXT,
    payment_id              INT NOT NULL,
    validation_message      TEXT,
    rejection_message       TEXT,
    created_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    CONSTRAINT fk_payment_id  FOREIGN KEY(payment_id) REFERENCES payments(id)
);

COMMIT;