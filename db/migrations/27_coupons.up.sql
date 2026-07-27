BEGIN;

CREATE TABLE coupons (
    id              SERIAL PRIMARY KEY,
    code            VARCHAR(32) NOT NULL,
    description     TEXT,
    type            VARCHAR(16) NOT NULL,          -- 'percent' | 'fixed_price'
    properties      JSONB NOT NULL,                -- {discount_pct} | {fixed_price, currency}
    enabled         BOOLEAN NOT NULL DEFAULT true,
    redeem_from     TIMESTAMPTZ NOT NULL DEFAULT now(),
    redeem_until    TIMESTAMPTZ NOT NULL,
    benefit_start   TIMESTAMPTZ,                   -- mode A: fixed window
    benefit_end     TIMESTAMPTZ,
    benefit_months  INT,                           -- mode B: N calendar months colored from the redemption month
    countries       TEXT[],                        -- optional scope; NULL = unrestricted
    max_redemptions INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX coupons_code_ci ON coupons (upper(code));

CREATE TABLE coupon_redemptions (
    id            SERIAL PRIMARY KEY,
    coupon_id     INT NOT NULL REFERENCES coupons(id),
    keycloak_id   VARCHAR(36) NOT NULL,
    redeemed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    benefit_start TIMESTAMPTZ NOT NULL,
    benefit_end   TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    UNIQUE (coupon_id, keycloak_id)
);
CREATE INDEX coupon_redemptions_active
    ON coupon_redemptions (keycloak_id, benefit_end) WHERE revoked_at IS NULL;

COMMIT;
