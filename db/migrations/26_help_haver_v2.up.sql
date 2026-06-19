BEGIN;

CREATE TABLE hh_requests (
    id             SERIAL       PRIMARY KEY,
    keycloak_id    VARCHAR(36)  NOT NULL,
    type           VARCHAR(20)  NOT NULL CHECK (type IN ('hh-hayal', 'hh-gimlaj', 'hh-other')),
    requested_pct  INTEGER      NOT NULL CHECK (requested_pct > 0 AND requested_pct <= 100),
    months         INTEGER      NOT NULL CHECK (months > 0 AND months <= 12),
    note           TEXT,
    status         VARCHAR(10)  NOT NULL DEFAULT 'REQUESTED' CHECK (status IN ('REQUESTED', 'APPROVED', 'DENIED')),
    rejection_note TEXT,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_hh_requests_keycloak_id ON hh_requests (keycloak_id);

-- Every grant is born from an approved request (1:1).
CREATE TABLE hh_grants (
    id           SERIAL       PRIMARY KEY,
    request_id   INTEGER      NOT NULL UNIQUE REFERENCES hh_requests (id),
    keycloak_id  VARCHAR(36)  NOT NULL,
    type         VARCHAR(20)  NOT NULL CHECK (type IN ('hh-hayal', 'hh-gimlaj', 'hh-other')),
    discount_pct INTEGER      NOT NULL CHECK (discount_pct > 0 AND discount_pct <= 100),
    start_date   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    end_date     TIMESTAMPTZ  NOT NULL,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    note         TEXT
);

CREATE INDEX idx_hh_grants_keycloak_id ON hh_grants (keycloak_id);

COMMIT;
