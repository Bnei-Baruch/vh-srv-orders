BEGIN;

ALTER TABLE orders
    DROP COLUMN IF EXISTS card_details_id;

COMMIT;