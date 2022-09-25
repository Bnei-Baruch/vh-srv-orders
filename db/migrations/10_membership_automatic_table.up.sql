BEGIN;

CREATE TABLE IF NOT EXISTS membership_automatic (
    id                              SERIAL PRIMARY KEY,
    order_id                        INT NOT NULL,
    payment_id                      INT NOT NULL,
    membership_id                   INT NOT NULL,
    created_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                      TIMESTAMP WITH TIME ZONE DEFAULT now(),
    CONSTRAINT fk_order_id          FOREIGN KEY(order_id) REFERENCES orders(id),
    CONSTRAINT fk_payment_id        FOREIGN KEY(payment_id) REFERENCES payments(id),
    CONSTRAINT fk_membership_id     FOREIGN KEY(membership_id) REFERENCES membership(id)
);

COMMIT;