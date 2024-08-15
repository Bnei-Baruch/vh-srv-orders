BEGIN;

CREATE TABLE IF NOT EXISTS monthly_charge (
    id          SERIAL PRIMARY KEY,
    start_date  TIMESTAMP WITH TIME ZONE DEFAULT null,
    end_date    TIMESTAMP WITH TIME ZONE DEFAULT null,
    month       INT NOT NULL,
    year        INT NOT NULL,
    status      TEXT NOT NULL,
    properties  JSON NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT now (),
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT null
);

CREATE TABLE IF NOT EXISTS monthly_charge_orders (
    charge_id   INT REFERENCES monthly_charge(id) NOT NULL,
    order_id    INT REFERENCES orders(id) NOT NULL,
    status      TEXT NOT NULL,
    keycloak_id TEXT NOT NULL,
    email       TEXT NOT NULL,
    comment     TEXT,
    payment_id  INT REFERENCES payments(id),
    PRIMARY KEY (charge_id, order_id)
);

COMMIT;