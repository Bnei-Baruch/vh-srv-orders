BEGIN;

CREATE TABLE IF NOT EXISTS transaction (
    id                          SERIAL PRIMARY KEY,
    order_id                    INT NOT NULL,
    payment_id                  INT NOT NULL,
    account_id                  INT NOT NULL,
    terminal_id                 TEXT NOT NULL,
    status                      INT NULL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    CONSTRAINT fk_order_id      FOREIGN KEY(order_id) REFERENCES orders(id),
    CONSTRAINT fk_payment_id    FOREIGN KEY(payment_id) REFERENCES payments(id),
    CONSTRAINT fk_account_id    FOREIGN KEY(account_id) REFERENCES accounts(id),
    CONSTRAINT fk_terminal_id   FOREIGN KEY(terminal_id) REFERENCES terminal(id)
);

COMMIT;