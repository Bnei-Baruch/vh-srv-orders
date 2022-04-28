BEGIN;

CREATE TABLE IF NOT EXISTS payments_pelecard (
    id                        SERIAL PRIMARY KEY,
    payment_method            TEXT NOT NULL,
    receipt                   TEXT,
    extra_info                TEXT,
    status                    TEXT,
    payment_id                INT NOT NULL,
    created_at                TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                TIMESTAMP WITH TIME ZONE DEFAULT now(),
    CONSTRAINT fk_payment_id  FOREIGN KEY(payment_id) REFERENCES payments(id)
);

COMMIT;