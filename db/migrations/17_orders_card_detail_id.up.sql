BEGIN;

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS card_details_id integer default 0;
COMMIT;