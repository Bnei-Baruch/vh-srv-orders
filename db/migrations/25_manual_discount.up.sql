BEGIN;

CREATE TABLE manual_discount (
    id          SERIAL       PRIMARY KEY,
    keycloak_id VARCHAR(36)  NOT NULL,
    start_date  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    end_date    TIMESTAMPTZ  NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    type        VARCHAR(20)  NOT NULL CHECK (type IN ('percent', 'fixed_price')),
    properties  JSONB        NOT NULL,
    note        TEXT
);

CREATE INDEX idx_manual_discount_keycloak_id ON manual_discount (keycloak_id);

COMMIT;
